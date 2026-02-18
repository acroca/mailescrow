package relay

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	netsmtp "net/smtp"
	"strconv"

	"github.com/albert/mailescrow/internal/store"
)

// Sender is the interface for sending emails upstream.
type Sender interface {
	Send(ctx context.Context, email *store.Email) error
}

// Relay sends approved emails to an upstream SMTP server.
type Relay struct {
	host     string
	port     int
	username string
	password string
	useTLS   bool
}

// New creates a new Relay configured to connect to the upstream SMTP server.
func New(host string, port int, username, password string, useTLS bool) *Relay {
	return &Relay{
		host:     host,
		port:     port,
		username: username,
		password: password,
		useTLS:   useTLS,
	}
}

// Send forwards an approved email via the upstream SMTP server using its raw message.
func (r *Relay) Send(ctx context.Context, email *store.Email) error {
	addr := net.JoinHostPort(r.host, strconv.Itoa(r.port))

	var c *netsmtp.Client
	var err error

	if r.useTLS {
		tlsConfig := &tls.Config{ServerName: r.host}
		conn, err := (&tls.Dialer{Config: tlsConfig}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return fmt.Errorf("tls dial: %w", err)
		}
		c, err = netsmtp.NewClient(conn, r.host)
		if err != nil {
			return fmt.Errorf("smtp client over tls: %w", err)
		}
	} else {
		c, err = netsmtp.Dial(addr)
		if err != nil {
			return fmt.Errorf("smtp dial: %w", err)
		}
		// Try STARTTLS if available.
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: r.host}); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		}
	}
	defer func() { _ = c.Close() }()

	if r.username != "" {
		auth := netsmtp.PlainAuth("", r.username, r.password, r.host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	if err := c.Mail(email.Sender); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	for _, rcpt := range email.Recipients {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("rcpt to %s: %w", rcpt, err)
		}
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := bytes.NewReader(email.RawMessage).WriteTo(w); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}

	return c.Quit()
}
