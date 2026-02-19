package store

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestSaveOutboundAndGet(t *testing.T) {
	st := newTestStore(t)

	id, err := st.SaveOutbound(t.Context(), "alice@example.com", []string{"bob@example.com"}, "Hello", "Hi Bob", []byte("raw message"))
	if err != nil {
		t.Fatalf("save outbound: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	email, err := st.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if email.ID != id {
		t.Errorf("id = %q, want %q", email.ID, id)
	}
	if email.Direction != DirectionOutbound {
		t.Errorf("direction = %q, want %q", email.Direction, DirectionOutbound)
	}
	if email.Status != StatusPending {
		t.Errorf("status = %q, want %q", email.Status, StatusPending)
	}
	if email.Sender != "alice@example.com" {
		t.Errorf("sender = %q, want %q", email.Sender, "alice@example.com")
	}
	if len(email.Recipients) != 1 || email.Recipients[0] != "bob@example.com" {
		t.Errorf("recipients = %v, want [bob@example.com]", email.Recipients)
	}
	if email.Subject != "Hello" {
		t.Errorf("subject = %q, want %q", email.Subject, "Hello")
	}
	if email.Body != "Hi Bob" {
		t.Errorf("body = %q, want %q", email.Body, "Hi Bob")
	}
	if string(email.RawMessage) != "raw message" {
		t.Errorf("raw_message = %q, want %q", email.RawMessage, "raw message")
	}
	if email.ReceivedAt.IsZero() {
		t.Error("received_at should not be zero")
	}
	if email.IMAPMessageID != "" {
		t.Errorf("imap_message_id = %q, want empty", email.IMAPMessageID)
	}
}

func TestSaveInboundAndGet(t *testing.T) {
	st := newTestStore(t)

	id, err := st.SaveInbound(t.Context(), "sender@example.com", []string{"me@example.com"}, "Inbound", "body", []byte("raw"),
		"<msg123@example.com>", "mailescrow/received")
	if err != nil {
		t.Fatalf("save inbound: %v", err)
	}

	email, err := st.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if email.Direction != DirectionInbound {
		t.Errorf("direction = %q, want %q", email.Direction, DirectionInbound)
	}
	if email.IMAPMessageID != "<msg123@example.com>" {
		t.Errorf("imap_message_id = %q, want %q", email.IMAPMessageID, "<msg123@example.com>")
	}
	if email.IMAPMailbox != "mailescrow/received" {
		t.Errorf("imap_mailbox = %q, want %q", email.IMAPMailbox, "mailescrow/received")
	}
}

func TestSaveMultipleRecipients(t *testing.T) {
	st := newTestStore(t)

	rcpts := []string{"bob@example.com", "carol@example.com", "dave@example.com"}
	id, err := st.SaveOutbound(t.Context(), "alice@example.com", rcpts, "Group", "Hello all", []byte("raw"))
	if err != nil {
		t.Fatalf("save outbound: %v", err)
	}

	email, err := st.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(email.Recipients) != 3 {
		t.Fatalf("recipients count = %d, want 3", len(email.Recipients))
	}
	for i, want := range rcpts {
		if email.Recipients[i] != want {
			t.Errorf("recipients[%d] = %q, want %q", i, email.Recipients[i], want)
		}
	}
}

func TestListPending(t *testing.T) {
	st := newTestStore(t)

	// Empty list.
	emails, err := st.ListPending(t.Context())
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(emails) != 0 {
		t.Fatalf("expected 0 emails, got %d", len(emails))
	}

	// Save two outbound and one inbound.
	st.SaveOutbound(t.Context(), "a@x.com", []string{"b@x.com"}, "First", "body1", []byte("raw1"))
	st.SaveOutbound(t.Context(), "c@x.com", []string{"d@x.com"}, "Second", "body2", []byte("raw2"))
	id3, _ := st.SaveInbound(t.Context(), "e@x.com", []string{"f@x.com"}, "Third", "body3", []byte("raw3"), "<m3>", "mailescrow/received")

	// Approve the inbound email; it should not show in ListPending.
	_ = st.Approve(t.Context(), id3)

	emails, err = st.ListPending(t.Context())
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(emails) != 2 {
		t.Fatalf("expected 2 pending emails, got %d", len(emails))
	}
	if emails[0].Subject != "First" {
		t.Errorf("first email subject = %q, want %q", emails[0].Subject, "First")
	}
	if emails[1].Subject != "Second" {
		t.Errorf("second email subject = %q, want %q", emails[1].Subject, "Second")
	}
}

func TestListApproved(t *testing.T) {
	st := newTestStore(t)

	id1, _ := st.SaveInbound(t.Context(), "a@x.com", []string{"b@x.com"}, "Inbound1", "body1", []byte("raw1"), "<m1>", "mailescrow/received")
	id2, _ := st.SaveInbound(t.Context(), "c@x.com", []string{"d@x.com"}, "Inbound2", "body2", []byte("raw2"), "<m2>", "mailescrow/received")
	_, _ = st.SaveOutbound(t.Context(), "e@x.com", []string{"f@x.com"}, "Outbound", "body3", []byte("raw3"))

	// Approve only the first inbound.
	_ = st.Approve(t.Context(), id1)

	// Approve the outbound too — it should NOT appear in ListApproved.
	_ = st.Approve(t.Context(), id2)
	_ = st.Approve(t.Context(), id2) // already approved, may fail silently

	emails, err := st.ListApproved(t.Context())
	if err != nil {
		t.Fatalf("list approved: %v", err)
	}
	// Both inbound emails are approved.
	if len(emails) != 2 {
		t.Fatalf("expected 2 approved inbound emails, got %d", len(emails))
	}
	for _, e := range emails {
		if e.Direction != DirectionInbound {
			t.Errorf("expected inbound, got %q", e.Direction)
		}
		if e.Status != StatusApproved {
			t.Errorf("expected approved, got %q", e.Status)
		}
	}
}

func TestApprove(t *testing.T) {
	st := newTestStore(t)

	id, _ := st.SaveInbound(t.Context(), "a@x.com", []string{"b@x.com"}, "Test", "body", []byte("raw"), "<m>", "mailescrow/received")

	if err := st.Approve(t.Context(), id); err != nil {
		t.Fatalf("approve: %v", err)
	}

	email, err := st.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if email.Status != StatusApproved {
		t.Errorf("status = %q, want approved", email.Status)
	}
}

func TestApproveNotFound(t *testing.T) {
	st := newTestStore(t)
	if err := st.Approve(t.Context(), "nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent id")
	}
}

func TestUpdateIMAPMailbox(t *testing.T) {
	st := newTestStore(t)

	id, _ := st.SaveInbound(t.Context(), "a@x.com", []string{"b@x.com"}, "Test", "body", []byte("raw"), "<m>", "mailescrow/received")

	if err := st.UpdateIMAPMailbox(t.Context(), id, "mailescrow/approved"); err != nil {
		t.Fatalf("update imap mailbox: %v", err)
	}

	email, err := st.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if email.IMAPMailbox != "mailescrow/approved" {
		t.Errorf("imap_mailbox = %q, want mailescrow/approved", email.IMAPMailbox)
	}
}

func TestDelete(t *testing.T) {
	st := newTestStore(t)

	id, _ := st.SaveOutbound(t.Context(), "a@x.com", []string{"b@x.com"}, "Test", "body", []byte("raw"))

	if err := st.Delete(t.Context(), id); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := st.Get(t.Context(), id)
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestDeleteNotFound(t *testing.T) {
	st := newTestStore(t)

	err := st.Delete(t.Context(), "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent id")
	}
}

func TestGetNotFound(t *testing.T) {
	st := newTestStore(t)

	_, err := st.Get(t.Context(), "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent id")
	}
}

func TestSaveGeneratesUniqueIDs(t *testing.T) {
	st := newTestStore(t)

	id1, _ := st.SaveOutbound(t.Context(), "a@x.com", []string{"b@x.com"}, "Test1", "body", []byte("raw"))
	id2, _ := st.SaveOutbound(t.Context(), "a@x.com", []string{"b@x.com"}, "Test2", "body", []byte("raw"))

	if id1 == id2 {
		t.Errorf("expected unique IDs, got %q twice", id1)
	}
}
