package integration

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/albert/mailescrow/internal/relay"
	smtpsrv "github.com/albert/mailescrow/internal/smtp"
	"github.com/albert/mailescrow/internal/store"
	"github.com/albert/mailescrow/internal/web"
)

// --- Mock upstream SMTP server ---

type receivedMessage struct {
	From string
	To   []string
	Data string
}

type upstreamSMTP struct {
	addr     string
	listener net.Listener

	mu       sync.Mutex
	received []receivedMessage
}

func startUpstreamSMTP(t *testing.T) *upstreamSMTP {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen upstream: %v", err)
	}
	u := &upstreamSMTP{addr: lis.Addr().String(), listener: lis}
	go u.serve()
	t.Cleanup(func() { lis.Close() })
	return u
}

func (u *upstreamSMTP) serve() {
	for {
		conn, err := u.listener.Accept()
		if err != nil {
			return
		}
		go u.handleConn(conn)
	}
}

func (u *upstreamSMTP) handleConn(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	write := func(msg string) { fmt.Fprintf(conn, "%s\r\n", msg) }

	write("220 upstream SMTP ready")

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
				u.mu.Lock()
				u.received = append(u.received, receivedMessage{
					From: from,
					To:   to,
					Data: data.String(),
				})
				u.mu.Unlock()
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
	parts := strings.SplitN(line, ":", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return line
}

func (u *upstreamSMTP) getReceived() []receivedMessage {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]receivedMessage, len(u.received))
	copy(out, u.received)
	return out
}

// --- Helpers ---

func freeAddr(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	addr := lis.Addr().String()
	lis.Close()
	return addr
}

func waitForPort(t *testing.T, addr string) {
	t.Helper()
	for range 100 {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("port %s never became available", addr)
}

// sendEmail sends a single email through the mailescrow SMTP server.
func sendEmail(t *testing.T, smtpAddr, from, to, subject, body string) {
	t.Helper()
	c, err := smtp.Dial(smtpAddr)
	if err != nil {
		t.Fatalf("smtp dial: %v", err)
	}
	auth := smtp.PlainAuth("", "user", "pass", "127.0.0.1")
	if err := c.Auth(auth); err != nil {
		t.Fatalf("smtp auth: %v", err)
	}
	if err := c.Mail(from); err != nil {
		t.Fatalf("mail from: %v", err)
	}
	if err := c.Rcpt(to); err != nil {
		t.Fatalf("rcpt to: %v", err)
	}
	w, err := c.Data()
	if err != nil {
		t.Fatalf("data: %v", err)
	}
	raw := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s", from, to, subject, body)
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close data: %v", err)
	}
	c.Quit()
}

// getBody fetches the HTML body from the web UI.
func getBody(t *testing.T, webAddr string) string {
	t.Helper()
	resp, err := http.Get("http://" + webAddr + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// extractID finds the email ID from the form whose action matches /email/<id>/<action>.
func extractID(body, action string) string {
	prefix := `action="/email/`
	suffix := "/" + action + `"`
	remaining := body
	for {
		idx := strings.Index(remaining, prefix)
		if idx < 0 {
			return ""
		}
		after := remaining[idx+len(prefix):]
		slash := strings.IndexByte(after, '/')
		if slash < 0 {
			return ""
		}
		id := after[:slash]
		if strings.HasPrefix(after[slash:], suffix) {
			return id
		}
		remaining = after
	}
}

// postAction posts to /email/{id}/{action} with no-redirect client.
func postAction(t *testing.T, webAddr, id, action string) {
	t.Helper()
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.PostForm("http://"+webAddr+"/email/"+id+"/"+action, url.Values{})
	if err != nil {
		t.Fatalf("POST /email/%s/%s: %v", id, action, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("POST /email/%s/%s: status %d, want 303", id, action, resp.StatusCode)
	}
}

// --- Integration tests ---

func TestApproveFlowEndToEnd(t *testing.T) {
	upstream := startUpstreamSMTP(t)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	upHost, upPortStr, _ := net.SplitHostPort(upstream.addr)
	var upPort int
	fmt.Sscanf(upPortStr, "%d", &upPort)
	r := relay.New(upHost, upPort, "", "", false)

	smtpAddr := freeAddr(t)
	smtpServer := smtpsrv.New(smtpAddr, "user", "pass", st)
	go smtpServer.ListenAndServe()
	t.Cleanup(func() { smtpServer.Close() })
	waitForPort(t, smtpAddr)

	webAddr := freeAddr(t)
	webServer := web.New(st, r)
	go webServer.Serve(webAddr)
	t.Cleanup(func() { webServer.Shutdown(t.Context()) }) //nolint
	waitForPort(t, webAddr)

	// Send an email.
	sendEmail(t, smtpAddr, "sender@example.com", "recipient@example.com", "Integration Test", "This is an integration test email.")

	// Check it appears in the web UI.
	body := getBody(t, webAddr)
	if !strings.Contains(body, "Integration Test") {
		t.Fatalf("web UI missing subject: %q", body)
	}
	if !strings.Contains(body, "sender@example.com") {
		t.Errorf("web UI missing sender")
	}

	// Approve via web UI.
	id := extractID(body, "approve")
	if id == "" {
		t.Fatal("could not extract email ID from web UI")
	}
	postAction(t, webAddr, id, "approve")

	// Verify upstream received the message.
	msgs := upstream.getReceived()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 upstream message, got %d", len(msgs))
	}
	if msgs[0].From != "sender@example.com" {
		t.Errorf("upstream from = %q, want sender@example.com", msgs[0].From)
	}
	if !strings.Contains(msgs[0].Data, "Subject: Integration Test") {
		t.Errorf("upstream data missing Subject header: %q", msgs[0].Data)
	}
	if !strings.Contains(msgs[0].Data, "integration test email") {
		t.Errorf("upstream data missing body: %q", msgs[0].Data)
	}

	// Verify email is gone from the web UI.
	body2 := getBody(t, webAddr)
	if strings.Contains(body2, "Integration Test") {
		t.Error("email still visible in web UI after approve")
	}
}

func TestRejectFlowEndToEnd(t *testing.T) {
	upstream := startUpstreamSMTP(t)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	upHost, upPortStr, _ := net.SplitHostPort(upstream.addr)
	var upPort int
	fmt.Sscanf(upPortStr, "%d", &upPort)
	r := relay.New(upHost, upPort, "", "", false)

	smtpAddr := freeAddr(t)
	smtpServer := smtpsrv.New(smtpAddr, "user", "pass", st)
	go smtpServer.ListenAndServe()
	t.Cleanup(func() { smtpServer.Close() })
	waitForPort(t, smtpAddr)

	webAddr := freeAddr(t)
	webServer := web.New(st, r)
	go webServer.Serve(webAddr)
	t.Cleanup(func() { webServer.Shutdown(t.Context()) }) //nolint
	waitForPort(t, webAddr)

	sendEmail(t, smtpAddr, "sender@example.com", "recipient@example.com", "Rejected Email", "This should be rejected.")

	body := getBody(t, webAddr)
	id := extractID(body, "reject")
	if id == "" {
		t.Fatal("could not extract email ID from web UI")
	}
	postAction(t, webAddr, id, "reject")

	// Verify upstream did NOT receive anything.
	msgs := upstream.getReceived()
	if len(msgs) != 0 {
		t.Errorf("expected 0 upstream messages after reject, got %d", len(msgs))
	}

	// Verify email is gone.
	body2 := getBody(t, webAddr)
	if strings.Contains(body2, "Rejected Email") {
		t.Error("email still visible in web UI after reject")
	}
}

func TestMultipleEmailsApproveAndReject(t *testing.T) {
	upstream := startUpstreamSMTP(t)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	upHost, upPortStr, _ := net.SplitHostPort(upstream.addr)
	var upPort int
	fmt.Sscanf(upPortStr, "%d", &upPort)
	r := relay.New(upHost, upPort, "", "", false)

	smtpAddr := freeAddr(t)
	smtpServer := smtpsrv.New(smtpAddr, "user", "pass", st)
	go smtpServer.ListenAndServe()
	t.Cleanup(func() { smtpServer.Close() })
	waitForPort(t, smtpAddr)

	webAddr := freeAddr(t)
	webServer := web.New(st, r)
	go webServer.Serve(webAddr)
	t.Cleanup(func() { webServer.Shutdown(t.Context()) }) //nolint
	waitForPort(t, webAddr)

	sendEmail(t, smtpAddr, "sender@example.com", "rcpt1@example.com", "Email One", "Body of Email One")
	sendEmail(t, smtpAddr, "sender@example.com", "rcpt2@example.com", "Email Two", "Body of Email Two")

	body := getBody(t, webAddr)
	if !strings.Contains(body, "Email One") || !strings.Contains(body, "Email Two") {
		t.Fatalf("web UI missing emails: %q", body)
	}

	// Find IDs for each email by scanning action URLs.
	// We need both approve and reject IDs; we approve Email One and reject Email Two.
	// Extract all IDs in order from approve actions.
	var ids []string
	remaining := body
	for {
		idx := strings.Index(remaining, `action="/email/`)
		if idx < 0 {
			break
		}
		rest := remaining[idx+len(`action="/email/`):]
		end := strings.IndexByte(rest, '/')
		if end < 0 {
			break
		}
		id := rest[:end]
		if len(ids) == 0 || ids[len(ids)-1] != id {
			ids = append(ids, id)
		}
		remaining = rest[end:]
	}

	// Get subjects for each ID.
	subjectForID := map[string]string{}
	for _, id := range ids {
		if strings.Contains(body[:strings.Index(body, id)], "Email One") {
			subjectForID[id] = "Email One"
		}
	}

	// Simpler: look for approve action URLs and pair with nearby subjects.
	// Just approve the first ID found and reject the second.
	if len(ids) < 2 {
		t.Fatalf("expected at least 2 email IDs, got %v", ids)
	}

	// Determine which ID belongs to which email.
	var approveID, rejectID string
	for _, id := range ids {
		// The id appears twice (approve + reject form). Check once.
		pos := strings.Index(body, id)
		before := body[:pos]
		if strings.LastIndex(before, "Email One") > strings.LastIndex(before, "Email Two") {
			approveID = id
		} else {
			rejectID = id
		}
		if approveID != "" && rejectID != "" {
			break
		}
	}

	if approveID == "" || rejectID == "" {
		// Fallback: just use first/second.
		approveID = ids[0]
		rejectID = ids[1]
	}

	postAction(t, webAddr, approveID, "approve")
	postAction(t, webAddr, rejectID, "reject")

	msgs := upstream.getReceived()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 upstream message, got %d", len(msgs))
	}

	body2 := getBody(t, webAddr)
	if strings.Contains(body2, "Email One") || strings.Contains(body2, "Email Two") {
		t.Error("emails still visible in web UI after approve/reject")
	}
}
