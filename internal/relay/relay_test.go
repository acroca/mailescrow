package relay

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/albert/mailescrow/internal/store"
)

// mockSMTPServer is a minimal SMTP server for testing the relay.
type mockSMTPServer struct {
	addr     string
	listener net.Listener

	mu       sync.Mutex
	received []receivedMessage
}

type receivedMessage struct {
	From string
	To   []string
	Data string
}

func newMockSMTPServer(t *testing.T) *mockSMTPServer {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	s := &mockSMTPServer{
		addr:     lis.Addr().String(),
		listener: lis,
	}

	go s.serve(t)
	t.Cleanup(func() { lis.Close() })

	return s
}

func (s *mockSMTPServer) serve(t *testing.T) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(t, conn)
	}
}

func (s *mockSMTPServer) handleConn(t *testing.T, conn net.Conn) {
	defer conn.Close()

	r := bufio.NewReader(conn)
	write := func(msg string) {
		fmt.Fprintf(conn, "%s\r\n", msg)
	}

	write("220 mock SMTP ready")

	var from string
	var to []string
	var data strings.Builder
	inData := false

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")

		if inData {
			if line == "." {
				inData = false
				s.mu.Lock()
				s.received = append(s.received, receivedMessage{
					From: from,
					To:   to,
					Data: data.String(),
				})
				s.mu.Unlock()
				write("250 OK")
				from = ""
				to = nil
				data.Reset()
				continue
			}
			data.WriteString(line)
			data.WriteString("\r\n")
			continue
		}

		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO"):
			write("250 Hello")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			from = extractAddr(line)
			write("250 OK")
		case strings.HasPrefix(upper, "RCPT TO:"):
			to = append(to, extractAddr(line))
			write("250 OK")
		case upper == "DATA":
			write("354 Start mail input")
			inData = true
		case upper == "QUIT":
			write("221 Bye")
			return
		default:
			write("500 Unknown command")
		}
	}
}

func extractAddr(line string) string {
	start := strings.Index(line, "<")
	end := strings.Index(line, ">")
	if start >= 0 && end > start {
		return line[start+1 : end]
	}
	// Fallback: take everything after the colon.
	parts := strings.SplitN(line, ":", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return line
}

func (s *mockSMTPServer) getReceived() []receivedMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]receivedMessage, len(s.received))
	copy(out, s.received)
	return out
}

func TestRelaySend(t *testing.T) {
	mock := newMockSMTPServer(t)

	host, portStr, _ := net.SplitHostPort(mock.addr)
	port := 0
	fmt.Sscanf(portStr, "%d", &port)

	r := New(host, port, "", "", false)

	email := &store.Email{
		ID:         "test-1",
		Sender:     "alice@example.com",
		Recipients: []string{"bob@example.com"},
		Subject:    "Test",
		Body:       "Hello",
		RawMessage: []byte("Subject: Test\r\n\r\nHello"),
		ReceivedAt: time.Now(),
	}

	if err := r.Send(t.Context(), email); err != nil {
		t.Fatalf("send: %v", err)
	}

	msgs := mock.getReceived()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 received message, got %d", len(msgs))
	}
	if msgs[0].From != "alice@example.com" {
		t.Errorf("from = %q, want %q", msgs[0].From, "alice@example.com")
	}
	if len(msgs[0].To) != 1 || msgs[0].To[0] != "bob@example.com" {
		t.Errorf("to = %v, want [bob@example.com]", msgs[0].To)
	}
	if !strings.Contains(msgs[0].Data, "Subject: Test") {
		t.Errorf("data = %q, expected to contain 'Subject: Test'", msgs[0].Data)
	}
}

func TestRelaySendMultipleRecipients(t *testing.T) {
	mock := newMockSMTPServer(t)

	host, portStr, _ := net.SplitHostPort(mock.addr)
	port := 0
	fmt.Sscanf(portStr, "%d", &port)

	r := New(host, port, "", "", false)

	email := &store.Email{
		ID:         "test-2",
		Sender:     "alice@example.com",
		Recipients: []string{"bob@example.com", "carol@example.com"},
		RawMessage: []byte("Subject: Multi\r\n\r\nHello all"),
		ReceivedAt: time.Now(),
	}

	if err := r.Send(t.Context(), email); err != nil {
		t.Fatalf("send: %v", err)
	}

	msgs := mock.getReceived()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 received message, got %d", len(msgs))
	}
	if len(msgs[0].To) != 2 {
		t.Errorf("expected 2 recipients, got %d", len(msgs[0].To))
	}
}

func TestRelaySendConnectionRefused(t *testing.T) {
	// Use a port that nothing is listening on.
	r := New("127.0.0.1", 1, "", "", false)

	email := &store.Email{
		ID:         "test-3",
		Sender:     "alice@example.com",
		Recipients: []string{"bob@example.com"},
		RawMessage: []byte("Subject: Test\r\n\r\nHello"),
	}

	err := r.Send(t.Context(), email)
	if err == nil {
		t.Fatal("expected error when connecting to closed port")
	}
}
