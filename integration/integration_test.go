package integration

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/albert/mailescrow/internal/relay"
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

func postAction(t *testing.T, webAddr, id, action string) {
	t.Helper()
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
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

func postAPIEmail(t *testing.T, webAddr, to, subject, body string) string {
	t.Helper()
	payload := map[string]interface{}{
		"to":      []string{to},
		"subject": subject,
		"body":    body,
	}
	b, _ := json.Marshal(payload)
	resp, err := http.Post("http://"+webAddr+"/api/emails", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST /api/emails: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/emails: status %d, want 201", resp.StatusCode)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result.ID
}

func getAPIEmails(t *testing.T, webAddr string) []map[string]interface{} {
	t.Helper()
	resp, err := http.Get("http://" + webAddr + "/api/emails")
	if err != nil {
		t.Fatalf("GET /api/emails: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/emails: status %d, want 200", resp.StatusCode)
	}
	var results []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return results
}

type testServer struct {
	webAddr string
	apiAddr string
}

func startTestServer(t *testing.T, st store.EmailStore, r relay.Sender) testServer {
	t.Helper()
	webAddr := freeAddr(t)
	apiAddr := freeAddr(t)
	srv := web.New(st, r, nil, "sender@example.com", "", "") // nil imapClient — no IMAP in integration tests
	go srv.Serve(webAddr)
	go srv.ServeAPI(apiAddr)
	t.Cleanup(func() { srv.Shutdown(t.Context()) }) //nolint:errcheck
	waitForPort(t, webAddr)
	waitForPort(t, apiAddr)
	return testServer{webAddr: webAddr, apiAddr: apiAddr}
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// --- Integration tests ---

// TestOutboundApproveFlow: POST /api/emails → approve in web UI → SMTP relay
func TestOutboundApproveFlow(t *testing.T) {
	upstream := startUpstreamSMTP(t)
	st := newTestStore(t)

	upHost, upPortStr, _ := net.SplitHostPort(upstream.addr)
	var upPort int
	fmt.Sscanf(upPortStr, "%d", &upPort)
	r := relay.New(upHost, upPort, "", "", false)

	srv := startTestServer(t, st, r)

	// Submit outbound email via API.
	postAPIEmail(t, srv.apiAddr, "recipient@example.com", "Integration Test", "This is an integration test email.")

	// Check it appears in web UI as pending.
	body := getBody(t, srv.webAddr)
	if !strings.Contains(body, "Integration Test") {
		t.Fatalf("web UI missing subject: %q", body)
	}
	if !strings.Contains(body, "sender@example.com") {
		t.Errorf("web UI missing sender")
	}
	if !strings.Contains(body, "outbound") {
		t.Errorf("web UI missing outbound badge")
	}

	// Approve via web UI.
	id := extractID(body, "approve")
	if id == "" {
		t.Fatal("could not extract email ID from web UI")
	}
	postAction(t, srv.webAddr, id, "approve")

	// Verify upstream received it.
	msgs := upstream.getReceived()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 upstream message, got %d", len(msgs))
	}
	if msgs[0].From != "sender@example.com" { // matches fromAddr passed to web.New
		t.Errorf("upstream from = %q, want sender@example.com", msgs[0].From)
	}
	if !strings.Contains(msgs[0].Data, "Subject: Integration Test") {
		t.Errorf("upstream data missing Subject header: %q", msgs[0].Data)
	}

	// Verify email is gone from web UI.
	body2 := getBody(t, srv.webAddr)
	if strings.Contains(body2, "Integration Test") {
		t.Error("email still visible in web UI after approve")
	}
}

// TestOutboundRejectFlow: POST /api/emails → reject → upstream gets nothing
func TestOutboundRejectFlow(t *testing.T) {
	upstream := startUpstreamSMTP(t)
	st := newTestStore(t)

	upHost, upPortStr, _ := net.SplitHostPort(upstream.addr)
	var upPort int
	fmt.Sscanf(upPortStr, "%d", &upPort)
	r := relay.New(upHost, upPort, "", "", false)

	srv := startTestServer(t, st, r)

	postAPIEmail(t, srv.apiAddr, "recipient@example.com", "Rejected Email", "This should be rejected.")

	body := getBody(t, srv.webAddr)
	id := extractID(body, "reject")
	if id == "" {
		t.Fatal("could not extract email ID from web UI")
	}
	postAction(t, srv.webAddr, id, "reject")

	// Upstream should NOT receive anything.
	msgs := upstream.getReceived()
	if len(msgs) != 0 {
		t.Errorf("expected 0 upstream messages after reject, got %d", len(msgs))
	}

	// Email is gone from UI.
	body2 := getBody(t, srv.webAddr)
	if strings.Contains(body2, "Rejected Email") {
		t.Error("email still visible in web UI after reject")
	}
}

// TestInboundApproveFlow: inject via SaveInbound → approve in UI → GET /api/emails
func TestInboundApproveFlow(t *testing.T) {
	st := newTestStore(t)
	r := relay.New("127.0.0.1", 1, "", "", false) // unused for inbound
	srv := startTestServer(t, st, r)

	// Simulate IMAP poller saving an inbound message.
	rawMsg := "From: external@example.com\r\nTo: me@example.com\r\nSubject: Inbound Test\r\nMessage-Id: <abc123@external.example.com>\r\n\r\nHello from outside!"
	_, err := st.SaveInbound(t.Context(),
		"external@example.com", []string{"me@example.com"},
		"Inbound Test", "Hello from outside!",
		[]byte(rawMsg),
		"<abc123@external.example.com>", "mailescrow/received",
	)
	if err != nil {
		t.Fatalf("save inbound: %v", err)
	}

	// Check it appears in web UI as inbound pending.
	body := getBody(t, srv.webAddr)
	if !strings.Contains(body, "Inbound Test") {
		t.Fatalf("web UI missing subject: %q", body)
	}
	if !strings.Contains(body, "inbound") {
		t.Errorf("web UI missing inbound badge")
	}
	if !strings.Contains(body, "Approve") {
		t.Errorf("web UI inbound approve button should say Approve")
	}

	// Approve via web UI.
	id := extractID(body, "approve")
	if id == "" {
		t.Fatal("could not extract email ID from web UI")
	}
	postAction(t, srv.webAddr, id, "approve")

	// Email should no longer be pending.
	body2 := getBody(t, srv.webAddr)
	if strings.Contains(body2, "Inbound Test") {
		t.Error("email still visible in pending web UI after approve")
	}

	// GET /api/emails should return the approved email.
	emails := getAPIEmails(t, srv.apiAddr)
	if len(emails) != 1 {
		t.Fatalf("expected 1 approved email, got %d", len(emails))
	}
	if emails[0]["subject"] != "Inbound Test" {
		t.Errorf("subject = %q, want Inbound Test", emails[0]["subject"])
	}
	if emails[0]["from"] != "external@example.com" {
		t.Errorf("from = %q, want external@example.com", emails[0]["from"])
	}

	// Second GET should return empty (consumed on read).
	emails2 := getAPIEmails(t, srv.apiAddr)
	if len(emails2) != 0 {
		t.Errorf("expected 0 emails on second GET, got %d", len(emails2))
	}
}

// TestInboundRejectFlow: inject via SaveInbound → reject → GET /api/emails returns nothing
func TestInboundRejectFlow(t *testing.T) {
	st := newTestStore(t)
	r := relay.New("127.0.0.1", 1, "", "", false)
	srv := startTestServer(t, st, r)

	rawMsg := "From: external@example.com\r\nTo: me@example.com\r\nSubject: Spam\r\nMessage-Id: <spam@example.com>\r\n\r\nBuy now!"
	_, err := st.SaveInbound(t.Context(),
		"external@example.com", []string{"me@example.com"},
		"Spam", "Buy now!",
		[]byte(rawMsg),
		"<spam@example.com>", "mailescrow/received",
	)
	if err != nil {
		t.Fatalf("save inbound: %v", err)
	}

	body := getBody(t, srv.webAddr)
	id := extractID(body, "reject")
	if id == "" {
		t.Fatal("could not extract email ID from web UI")
	}
	postAction(t, srv.webAddr, id, "reject")

	// GET /api/emails should return nothing.
	emails := getAPIEmails(t, srv.apiAddr)
	if len(emails) != 0 {
		t.Errorf("expected 0 emails after reject, got %d", len(emails))
	}
}

// TestPendingCount: GET /api/emails/pending/count returns the right number
func TestPendingCount(t *testing.T) {
	st := newTestStore(t)
	r := relay.New("127.0.0.1", 1, "", "", false)
	srv := startTestServer(t, st, r)

	getPendingCount := func() int {
		t.Helper()
		resp, err := http.Get("http://" + srv.apiAddr + "/api/emails/pending/count")
		if err != nil {
			t.Fatalf("GET /api/emails/pending/count: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/emails/pending/count: status %d, want 200", resp.StatusCode)
		}
		var result struct {
			Count int `json:"count"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return result.Count
	}

	if n := getPendingCount(); n != 0 {
		t.Errorf("initial count = %d, want 0", n)
	}

	postAPIEmail(t, srv.apiAddr, "b@example.com", "First", "body")
	if n := getPendingCount(); n != 1 {
		t.Errorf("after 1 email count = %d, want 1", n)
	}

	postAPIEmail(t, srv.apiAddr, "b@example.com", "Second", "body")
	if n := getPendingCount(); n != 2 {
		t.Errorf("after 2 emails count = %d, want 2", n)
	}

	body := getBody(t, srv.webAddr)
	id := extractID(body, "reject")
	postAction(t, srv.webAddr, id, "reject")
	if n := getPendingCount(); n != 1 {
		t.Errorf("after reject count = %d, want 1", n)
	}
}

// TestMixedApproveAndReject: multiple outbound emails with mixed actions
func TestMixedApproveAndReject(t *testing.T) {
	upstream := startUpstreamSMTP(t)
	st := newTestStore(t)

	upHost, upPortStr, _ := net.SplitHostPort(upstream.addr)
	var upPort int
	fmt.Sscanf(upPortStr, "%d", &upPort)
	r := relay.New(upHost, upPort, "", "", false)

	srv := startTestServer(t, st, r)

	postAPIEmail(t, srv.apiAddr, "rcpt1@example.com", "Email One", "Body of Email One")
	postAPIEmail(t, srv.apiAddr, "rcpt2@example.com", "Email Two", "Body of Email Two")

	body := getBody(t, srv.webAddr)
	if !strings.Contains(body, "Email One") || !strings.Contains(body, "Email Two") {
		t.Fatalf("web UI missing emails: %q", body)
	}

	// Extract all email IDs in order.
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
	if len(ids) < 2 {
		t.Fatalf("expected at least 2 email IDs, got %v", ids)
	}

	// Determine which ID belongs to which email.
	var approveID, rejectID string
	for _, id := range ids {
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
		approveID = ids[0]
		rejectID = ids[1]
	}

	postAction(t, srv.webAddr, approveID, "approve")
	postAction(t, srv.webAddr, rejectID, "reject")

	msgs := upstream.getReceived()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 upstream message, got %d", len(msgs))
	}

	body2 := getBody(t, srv.webAddr)
	if strings.Contains(body2, "Email One") || strings.Contains(body2, "Email Two") {
		t.Error("emails still visible in web UI after approve/reject")
	}
}
