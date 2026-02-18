package smtp

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/albert/mailescrow/internal/store"
)

// mockStore implements store.EmailStore for testing.
type mockStore struct {
	saved []savedEmail
}

type savedEmail struct {
	Sender     string
	Recipients []string
	Subject    string
	Body       string
	RawMessage []byte
}

func (m *mockStore) Save(_ context.Context, sender string, recipients []string, subject, body string, rawMessage []byte) (string, error) {
	m.saved = append(m.saved, savedEmail{
		Sender:     sender,
		Recipients: recipients,
		Subject:    subject,
		Body:       body,
		RawMessage: rawMessage,
	})
	return fmt.Sprintf("test-%d", len(m.saved)), nil
}

func (m *mockStore) List(_ context.Context) ([]store.Email, error) { return nil, nil }
func (m *mockStore) Get(_ context.Context, id string) (*store.Email, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockStore) Delete(_ context.Context, id string) error { return nil }

func startTestServer(t *testing.T, ms *mockStore) string {
	t.Helper()

	// Find a free port.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := lis.Addr().String()
	lis.Close()

	srv := New(addr, "testuser", "testpass", ms)
	go srv.ListenAndServe()
	t.Cleanup(func() { srv.Close() })

	// Wait for server to be ready.
	for range 50 {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return addr
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server did not start on %s", addr)
	return ""
}

func TestSMTPReceiveEmail(t *testing.T) {
	ms := &mockStore{}
	addr := startTestServer(t, ms)

	c, err := smtp.Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// Authenticate.
	auth := smtp.PlainAuth("", "testuser", "testpass", "127.0.0.1")
	if err := c.Auth(auth); err != nil {
		t.Fatalf("auth: %v", err)
	}

	// Send mail.
	if err := c.Mail("sender@example.com"); err != nil {
		t.Fatalf("mail from: %v", err)
	}
	if err := c.Rcpt("recipient@example.com"); err != nil {
		t.Fatalf("rcpt to: %v", err)
	}

	w, err := c.Data()
	if err != nil {
		t.Fatalf("data: %v", err)
	}

	msg := "Subject: Test Email\r\n\r\nHello, this is a test."
	if _, err := w.Write([]byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close data: %v", err)
	}
	c.Quit()

	// Verify the email was stored.
	if len(ms.saved) != 1 {
		t.Fatalf("expected 1 saved email, got %d", len(ms.saved))
	}

	saved := ms.saved[0]
	if saved.Sender != "sender@example.com" {
		t.Errorf("sender = %q, want %q", saved.Sender, "sender@example.com")
	}
	if len(saved.Recipients) != 1 || saved.Recipients[0] != "recipient@example.com" {
		t.Errorf("recipients = %v, want [recipient@example.com]", saved.Recipients)
	}
	if saved.Subject != "Test Email" {
		t.Errorf("subject = %q, want %q", saved.Subject, "Test Email")
	}
	if !strings.Contains(saved.Body, "Hello, this is a test") {
		t.Errorf("body = %q, expected to contain 'Hello, this is a test'", saved.Body)
	}
}

func TestSMTPMultipleRecipients(t *testing.T) {
	ms := &mockStore{}
	addr := startTestServer(t, ms)

	c, err := smtp.Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	auth := smtp.PlainAuth("", "testuser", "testpass", "127.0.0.1")
	if err := c.Auth(auth); err != nil {
		t.Fatalf("auth: %v", err)
	}

	if err := c.Mail("sender@example.com"); err != nil {
		t.Fatalf("mail from: %v", err)
	}
	if err := c.Rcpt("bob@example.com"); err != nil {
		t.Fatalf("rcpt to bob: %v", err)
	}
	if err := c.Rcpt("carol@example.com"); err != nil {
		t.Fatalf("rcpt to carol: %v", err)
	}

	w, err := c.Data()
	if err != nil {
		t.Fatalf("data: %v", err)
	}
	w.Write([]byte("Subject: Multi\r\n\r\nMultiple recipients"))
	w.Close()
	c.Quit()

	if len(ms.saved) != 1 {
		t.Fatalf("expected 1 saved email, got %d", len(ms.saved))
	}
	if len(ms.saved[0].Recipients) != 2 {
		t.Errorf("expected 2 recipients, got %d", len(ms.saved[0].Recipients))
	}
}

func TestSMTPAuthRequired(t *testing.T) {
	ms := &mockStore{}
	addr := startTestServer(t, ms)

	c, err := smtp.Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// Try to send without auth — should fail.
	err = c.Mail("sender@example.com")
	if err == nil {
		t.Fatal("expected error when sending without auth")
	}
}

func TestSMTPBadCredentials(t *testing.T) {
	ms := &mockStore{}
	addr := startTestServer(t, ms)

	c, err := smtp.Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	auth := smtp.PlainAuth("", "wrong", "creds", "127.0.0.1")
	err = c.Auth(auth)
	if err == nil {
		t.Fatal("expected error with bad credentials")
	}
}

func TestParseMessage(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantSubject string
		wantBody    string
	}{
		{
			name:        "standard message",
			raw:         "Subject: Hello World\r\n\r\nThis is the body.",
			wantSubject: "Hello World",
			wantBody:    "This is the body.",
		},
		{
			name:        "no subject",
			raw:         "From: test@example.com\r\n\r\nBody only.",
			wantSubject: "(no subject)",
			wantBody:    "Body only.",
		},
		{
			name:        "invalid message",
			raw:         "not a valid email",
			wantSubject: "(unknown)",
			wantBody:    "not a valid email",
		},
		{
			name:        "encoded subject",
			raw:         "Subject: =?UTF-8?B?SGVsbG8gV29ybGQ=?=\r\n\r\nBody",
			wantSubject: "Hello World",
			wantBody:    "Body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subject, body := parseMessage([]byte(tt.raw))
			if subject != tt.wantSubject {
				t.Errorf("subject = %q, want %q", subject, tt.wantSubject)
			}
			if body != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}
