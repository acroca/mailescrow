package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const (
	DirectionOutbound = "outbound"
	DirectionInbound  = "inbound"

	StatusPending  = "pending"
	StatusApproved = "approved"
)

// Email represents a held email in the store.
type Email struct {
	ID            string
	Direction     string // "outbound" | "inbound"
	Status        string // "pending" | "approved"
	Sender        string
	Recipients    []string
	Subject       string
	Body          string
	RawMessage    []byte
	ReceivedAt    time.Time
	IMAPMessageID string // inbound only
	IMAPMailbox   string // inbound only, current IMAP folder
}

// EmailStore is the interface for email persistence operations.
type EmailStore interface {
	SaveOutbound(ctx context.Context, sender string, recipients []string, subject, body string, rawMessage []byte) (string, error)
	SaveInbound(ctx context.Context, sender string, recipients []string, subject, body string, rawMessage []byte, imapMessageID, imapMailbox string) (string, error)
	ListPending(ctx context.Context) ([]Email, error)
	ListApproved(ctx context.Context) ([]Email, error)
	Get(ctx context.Context, id string) (*Email, error)
	Approve(ctx context.Context, id string) error
	UpdateIMAPMailbox(ctx context.Context, id, mailbox string) error
	Delete(ctx context.Context, id string) error
}

// Store manages email persistence in SQLite.
type Store struct {
	db *sql.DB
}

// New opens (or creates) the SQLite database at path and initializes the schema.
func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS emails (
			id              TEXT PRIMARY KEY,
			direction       TEXT NOT NULL,
			status          TEXT NOT NULL,
			sender          TEXT NOT NULL,
			recipients      TEXT NOT NULL,
			subject         TEXT NOT NULL,
			body            TEXT NOT NULL,
			raw_message     BLOB NOT NULL,
			received_at     TIMESTAMP NOT NULL,
			imap_message_id TEXT,
			imap_mailbox    TEXT
		)
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create table: %w", err)
	}

	return &Store{db: db}, nil
}

// SaveOutbound persists a new outbound email, assigning it a UUID.
func (s *Store) SaveOutbound(ctx context.Context, sender string, recipients []string, subject, body string, rawMessage []byte) (string, error) {
	id := uuid.New().String()
	recipientsJSON, err := json.Marshal(recipients)
	if err != nil {
		return "", fmt.Errorf("marshal recipients: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO emails (id, direction, status, sender, recipients, subject, body, raw_message, received_at, imap_message_id, imap_mailbox)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL)`,
		id, DirectionOutbound, StatusPending, sender, string(recipientsJSON), subject, body, rawMessage, time.Now().UTC(),
	)
	if err != nil {
		return "", fmt.Errorf("insert email: %w", err)
	}
	return id, nil
}

// SaveInbound persists a new inbound email from IMAP polling.
func (s *Store) SaveInbound(ctx context.Context, sender string, recipients []string, subject, body string, rawMessage []byte, imapMessageID, imapMailbox string) (string, error) {
	id := uuid.New().String()
	recipientsJSON, err := json.Marshal(recipients)
	if err != nil {
		return "", fmt.Errorf("marshal recipients: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO emails (id, direction, status, sender, recipients, subject, body, raw_message, received_at, imap_message_id, imap_mailbox)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, DirectionInbound, StatusPending, sender, string(recipientsJSON), subject, body, rawMessage, time.Now().UTC(), imapMessageID, imapMailbox,
	)
	if err != nil {
		return "", fmt.Errorf("insert email: %w", err)
	}
	return id, nil
}

// ListPending returns all pending emails (for web UI).
func (s *Store) ListPending(ctx context.Context) ([]Email, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, direction, status, sender, recipients, subject, body, raw_message, received_at, imap_message_id, imap_mailbox
		 FROM emails WHERE status = ? ORDER BY received_at ASC`,
		StatusPending,
	)
	if err != nil {
		return nil, fmt.Errorf("query emails: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanEmails(rows)
}

// ListApproved returns all approved inbound emails (for GET /api/emails).
func (s *Store) ListApproved(ctx context.Context) ([]Email, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, direction, status, sender, recipients, subject, body, raw_message, received_at, imap_message_id, imap_mailbox
		 FROM emails WHERE direction = ? AND status = ? ORDER BY received_at ASC`,
		DirectionInbound, StatusApproved,
	)
	if err != nil {
		return nil, fmt.Errorf("query emails: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanEmails(rows)
}

// Get retrieves a single email by ID.
func (s *Store) Get(ctx context.Context, id string) (*Email, error) {
	var e Email
	var recipientsJSON string
	var imapMessageID, imapMailbox sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, direction, status, sender, recipients, subject, body, raw_message, received_at, imap_message_id, imap_mailbox
		 FROM emails WHERE id = ?`, id,
	).Scan(&e.ID, &e.Direction, &e.Status, &e.Sender, &recipientsJSON, &e.Subject, &e.Body, &e.RawMessage, &e.ReceivedAt, &imapMessageID, &imapMailbox)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("email not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("query email: %w", err)
	}
	if err := json.Unmarshal([]byte(recipientsJSON), &e.Recipients); err != nil {
		return nil, fmt.Errorf("unmarshal recipients: %w", err)
	}
	e.IMAPMessageID = imapMessageID.String
	e.IMAPMailbox = imapMailbox.String
	return &e, nil
}

// Approve sets an email's status to approved.
func (s *Store) Approve(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE emails SET status = ? WHERE id = ?`, StatusApproved, id)
	if err != nil {
		return fmt.Errorf("approve email: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("email not found: %s", id)
	}
	return nil
}

// UpdateIMAPMailbox updates the IMAP mailbox field for an email.
func (s *Store) UpdateIMAPMailbox(ctx context.Context, id, mailbox string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE emails SET imap_mailbox = ? WHERE id = ?`, mailbox, id)
	if err != nil {
		return fmt.Errorf("update imap mailbox: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("email not found: %s", id)
	}
	return nil
}

// Delete removes an email by ID.
func (s *Store) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM emails WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete email: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("email not found: %s", id)
	}
	return nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func scanEmails(rows *sql.Rows) ([]Email, error) {
	var emails []Email
	for rows.Next() {
		var e Email
		var recipientsJSON string
		var imapMessageID, imapMailbox sql.NullString
		if err := rows.Scan(&e.ID, &e.Direction, &e.Status, &e.Sender, &recipientsJSON, &e.Subject, &e.Body, &e.RawMessage, &e.ReceivedAt, &imapMessageID, &imapMailbox); err != nil {
			return nil, fmt.Errorf("scan email: %w", err)
		}
		if err := json.Unmarshal([]byte(recipientsJSON), &e.Recipients); err != nil {
			return nil, fmt.Errorf("unmarshal recipients: %w", err)
		}
		e.IMAPMessageID = imapMessageID.String
		e.IMAPMailbox = imapMailbox.String
		emails = append(emails, e)
	}
	return emails, rows.Err()
}
