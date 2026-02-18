package smtp

import (
	"bytes"
	"context"
	"io"
	"log"
	"mime"
	"net/mail"
	"strings"

	"github.com/albert/mailescrow/internal/store"
	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"
)

// Server wraps the go-smtp server with our backend.
type Server struct {
	srv *gosmtp.Server
}

// New creates a new SMTP server that stores received mail in the given store.
func New(addr, authUser, authPass string, st store.EmailStore) *Server {
	be := &backend{
		store:    st,
		authUser: authUser,
		authPass: authPass,
	}

	srv := gosmtp.NewServer(be)
	srv.Addr = addr
	srv.Domain = "mailescrow"
	srv.AllowInsecureAuth = true

	return &Server{srv: srv}
}

// ListenAndServe starts the SMTP server.
func (s *Server) ListenAndServe() error {
	return s.srv.ListenAndServe()
}

// Close gracefully shuts down the SMTP server.
func (s *Server) Close() error {
	return s.srv.Close()
}

// backend implements gosmtp.Backend.
type backend struct {
	store    store.EmailStore
	authUser string
	authPass string
}

func (b *backend) NewSession(c *gosmtp.Conn) (gosmtp.Session, error) {
	return &session{backend: b}, nil
}

// session implements gosmtp.Session.
type session struct {
	backend    *backend
	authed     bool
	sender     string
	recipients []string
}

func (s *session) AuthMechanisms() []string {
	return []string{sasl.Plain}
}

func (s *session) Auth(mech string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(identity, username, password string) error {
		if username == s.backend.authUser && password == s.backend.authPass {
			s.authed = true
			return nil
		}
		return &gosmtp.SMTPError{
			Code:    535,
			Message: "Authentication failed",
		}
	}), nil
}

func (s *session) Mail(from string, opts *gosmtp.MailOptions) error {
	if !s.authed {
		return &gosmtp.SMTPError{
			Code:    530,
			Message: "Authentication required",
		}
	}
	s.sender = from
	return nil
}

func (s *session) Rcpt(to string, opts *gosmtp.RcptOptions) error {
	s.recipients = append(s.recipients, to)
	return nil
}

func (s *session) Data(r io.Reader) error {
	rawMessage, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	subject, body := parseMessage(rawMessage)

	id, err := s.backend.store.Save(context.Background(), s.sender, s.recipients, subject, body, rawMessage)
	if err != nil {
		return err
	}

	log.Printf("Received email %s from %s to %v (subject: %s)", id, s.sender, s.recipients, subject)
	return nil
}

func (s *session) Reset() {
	s.sender = ""
	s.recipients = nil
}

func (s *session) Logout() error {
	return nil
}

// parseMessage extracts the subject and text body from a raw email message.
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
