package imap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"os"
	"strconv"
	"strings"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const (
	FolderReceived = "mailescrow/received"
	FolderApproved = "mailescrow/approved"
	FolderRejected = "mailescrow/rejected"
	FolderRead     = "mailescrow/read"
)

// Client polls an IMAP server for inbound email and manages mailescrow folders.
type Client struct {
	host     string
	username string
	password string
	port     int
	useTLS   bool
}

// FetchedEmail carries parsed data from a fetched IMAP message.
type FetchedEmail struct {
	MessageID  string
	Sender     string
	Recipients []string
	Subject    string
	Body       string
	RawMessage []byte
}

// New creates a new Client.
func New(host string, port int, username, password string, useTLS bool) *Client {
	return &Client{
		host:     host,
		username: username,
		password: password,
		port:     port,
		useTLS:   useTLS,
	}
}

func (c *Client) connect() (*imapclient.Client, error) {
	addr := net.JoinHostPort(c.host, strconv.Itoa(c.port))

	var opts *imapclient.Options
	if os.Getenv("MAILESCROW_IMAP_DEBUG") != "" {
		opts = &imapclient.Options{DebugWriter: os.Stderr}
	}

	var ic *imapclient.Client
	var err error
	if c.useTLS {
		ic, err = imapclient.DialTLS(addr, opts)
	} else {
		ic, err = imapclient.DialInsecure(addr, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	if err := ic.Login(c.username, c.password).Wait(); err != nil {
		_ = ic.Close()
		return nil, fmt.Errorf("login: %w", err)
	}
	return ic, nil
}

// EnsureFolders creates the four mailescrow/* folders if they don't exist.
// It uses CREATE-or-ignore rather than LIST to avoid Gmail closing the
// connection when the wildcard pattern matches nothing.
func (c *Client) EnsureFolders(_ context.Context) error {
	ic, err := c.connect()
	if err != nil {
		return err
	}
	defer func() { _ = ic.Logout().Wait() }()

	folders := []string{FolderReceived, FolderApproved, FolderRejected, FolderRead}
	for _, folder := range folders {
		if err := ic.Create(folder, nil).Wait(); err != nil {
			var imapErr *goimap.Error
			if errors.As(err, &imapErr) && imapErr.Code == goimap.ResponseCodeAlreadyExists {
				continue
			}
			return fmt.Errorf("create folder %s: %w", folder, err)
		}
	}
	return nil
}

// Poll fetches messages from INBOX, skipping any whose Message-Id is in
// knownMessageIDs, and moves new ones to mailescrow/received.
func (c *Client) Poll(_ context.Context, knownMessageIDs []string) ([]FetchedEmail, error) {
	ic, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer func() { _ = ic.Logout().Wait() }()

	if _, err := ic.Select("INBOX", nil).Wait(); err != nil {
		return nil, fmt.Errorf("select INBOX: %w", err)
	}

	// Search all non-deleted messages.
	searchData, err := ic.UIDSearch(&goimap.SearchCriteria{
		NotFlag: []goimap.Flag{goimap.FlagDeleted},
	}, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("search INBOX: %w", err)
	}

	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}

	// Fetch the raw body of all messages.
	var bodySectionItem goimap.FetchItemBodySection
	bodySectionItem.Peek = true // don't mark as \Seen
	fetchOptions := &goimap.FetchOptions{
		UID:         true,
		BodySection: []*goimap.FetchItemBodySection{&bodySectionItem},
	}
	uidSet := goimap.UIDSetNum(uids...)
	messages, err := ic.Fetch(uidSet, fetchOptions).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	knownIDs := make(map[string]bool, len(knownMessageIDs))
	for _, id := range knownMessageIDs {
		knownIDs[id] = true
	}

	var fetched []FetchedEmail
	var newUIDs []goimap.UID

	for _, msg := range messages {
		raw := msg.FindBodySection(&bodySectionItem)
		if len(raw) == 0 {
			continue
		}
		msgID := extractMessageID(raw)
		if knownIDs[msgID] {
			continue
		}
		subject, body := parseMessage(raw)
		sender, recipients := parseAddresses(raw)
		fetched = append(fetched, FetchedEmail{
			MessageID:  msgID,
			Sender:     sender,
			Recipients: recipients,
			Subject:    subject,
			Body:       body,
			RawMessage: raw,
		})
		newUIDs = append(newUIDs, msg.UID)
	}

	if len(newUIDs) > 0 {
		newSet := goimap.UIDSetNum(newUIDs...)
		if _, err := ic.Move(newSet, FolderReceived).Wait(); err != nil {
			return nil, fmt.Errorf("move to %s: %w", FolderReceived, err)
		}
	}

	return fetched, nil
}

// MoveMessage finds a message by Message-Id in fromMailbox and moves it to toMailbox.
func (c *Client) MoveMessage(_ context.Context, messageID, fromMailbox, toMailbox string) error {
	ic, err := c.connect()
	if err != nil {
		return err
	}
	defer func() { _ = ic.Logout().Wait() }()

	if _, err := ic.Select(fromMailbox, nil).Wait(); err != nil {
		return fmt.Errorf("select %s: %w", fromMailbox, err)
	}

	searchData, err := ic.UIDSearch(&goimap.SearchCriteria{
		Header: []goimap.SearchCriteriaHeaderField{
			{Key: "Message-Id", Value: messageID},
		},
	}, nil).Wait()
	if err != nil {
		return fmt.Errorf("search for message: %w", err)
	}

	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		return fmt.Errorf("message not found in %s: %s", fromMailbox, messageID)
	}

	uidSet := goimap.UIDSetNum(uids...)
	if _, err := ic.Move(uidSet, toMailbox).Wait(); err != nil {
		return fmt.Errorf("move message: %w", err)
	}
	return nil
}

func extractMessageID(raw []byte) string {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	return msg.Header.Get("Message-Id")
}

func parseAddresses(raw []byte) (sender string, recipients []string) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return "", nil
	}
	fromAddrs, err := msg.Header.AddressList("From")
	if err == nil && len(fromAddrs) > 0 {
		sender = fromAddrs[0].Address
	}
	toAddrs, _ := msg.Header.AddressList("To")
	for _, addr := range toAddrs {
		recipients = append(recipients, addr.Address)
	}
	return sender, recipients
}

func parseMessage(raw []byte) (subject, body string) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return "(unknown)", string(raw)
	}
	subject = msg.Header.Get("Subject")
	if subject != "" {
		decoded, err := new(mime.WordDecoder).DecodeHeader(subject)
		if err == nil {
			subject = decoded
		}
	}
	if subject == "" {
		subject = "(no subject)"
	}
	bodyBytes, err := io.ReadAll(msg.Body)
	if err != nil {
		return subject, ""
	}
	body = strings.TrimSpace(string(bodyBytes))
	return subject, body
}
