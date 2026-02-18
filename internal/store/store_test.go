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

func TestSaveAndGet(t *testing.T) {
	st := newTestStore(t)

	id, err := st.Save(t.Context(), "alice@example.com", []string{"bob@example.com"}, "Hello", "Hi Bob", []byte("raw message"))
	if err != nil {
		t.Fatalf("save: %v", err)
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
}

func TestSaveMultipleRecipients(t *testing.T) {
	st := newTestStore(t)

	rcpts := []string{"bob@example.com", "carol@example.com", "dave@example.com"}
	id, err := st.Save(t.Context(), "alice@example.com", rcpts, "Group", "Hello all", []byte("raw"))
	if err != nil {
		t.Fatalf("save: %v", err)
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

func TestList(t *testing.T) {
	st := newTestStore(t)

	// Empty list.
	emails, err := st.List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(emails) != 0 {
		t.Fatalf("expected 0 emails, got %d", len(emails))
	}

	// Save two emails.
	st.Save(t.Context(), "a@x.com", []string{"b@x.com"}, "First", "body1", []byte("raw1"))
	st.Save(t.Context(), "c@x.com", []string{"d@x.com"}, "Second", "body2", []byte("raw2"))

	emails, err = st.List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(emails) != 2 {
		t.Fatalf("expected 2 emails, got %d", len(emails))
	}

	// Verify ordering (ASC by received_at).
	if emails[0].Subject != "First" {
		t.Errorf("first email subject = %q, want %q", emails[0].Subject, "First")
	}
	if emails[1].Subject != "Second" {
		t.Errorf("second email subject = %q, want %q", emails[1].Subject, "Second")
	}
}

func TestDelete(t *testing.T) {
	st := newTestStore(t)

	id, _ := st.Save(t.Context(), "a@x.com", []string{"b@x.com"}, "Test", "body", []byte("raw"))

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

	id1, _ := st.Save(t.Context(), "a@x.com", []string{"b@x.com"}, "Test1", "body", []byte("raw"))
	id2, _ := st.Save(t.Context(), "a@x.com", []string{"b@x.com"}, "Test2", "body", []byte("raw"))

	if id1 == id2 {
		t.Errorf("expected unique IDs, got %q twice", id1)
	}
}
