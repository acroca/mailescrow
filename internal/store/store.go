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

// Email represents a held email in the store.
type Email struct {
	ID         string
	Sender     string
	Recipients []string
	Subject    string
	Body       string
	RawMessage []byte
	ReceivedAt time.Time
}

// EmailStore is the interface for email persistence operations.
type EmailStore interface {
	Save(ctx context.Context, sender string, recipients []string, subject, body string, rawMessage []byte) (string, error)
	List(ctx context.Context) ([]Email, error)
	Get(ctx context.Context, id string) (*Email, error)
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
			id          TEXT PRIMARY KEY,
			sender      TEXT NOT NULL,
			recipients  TEXT NOT NULL,
			subject     TEXT NOT NULL,
			body        TEXT NOT NULL,
			raw_message BLOB NOT NULL,
			received_at TIMESTAMP NOT NULL
		)
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create table: %w", err)
	}

	return &Store{db: db}, nil
}

// Save persists a new email, assigning it a UUID.
func (s *Store) Save(ctx context.Context, sender string, recipients []string, subject, body string, rawMessage []byte) (string, error) {
	id := uuid.New().String()
	recipientsJSON, err := json.Marshal(recipients)
	if err != nil {
		return "", fmt.Errorf("marshal recipients: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO emails (id, sender, recipients, subject, body, raw_message, received_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, sender, string(recipientsJSON), subject, body, rawMessage, time.Now().UTC(),
	)
	if err != nil {
		return "", fmt.Errorf("insert email: %w", err)
	}
	return id, nil
}

// List returns all pending emails.
func (s *Store) List(ctx context.Context) ([]Email, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, sender, recipients, subject, body, raw_message, received_at FROM emails ORDER BY received_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("query emails: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var emails []Email
	for rows.Next() {
		var e Email
		var recipientsJSON string
		if err := rows.Scan(&e.ID, &e.Sender, &recipientsJSON, &e.Subject, &e.Body, &e.RawMessage, &e.ReceivedAt); err != nil {
			return nil, fmt.Errorf("scan email: %w", err)
		}
		if err := json.Unmarshal([]byte(recipientsJSON), &e.Recipients); err != nil {
			return nil, fmt.Errorf("unmarshal recipients: %w", err)
		}
		emails = append(emails, e)
	}
	return emails, rows.Err()
}

// Get retrieves a single email by ID.
func (s *Store) Get(ctx context.Context, id string) (*Email, error) {
	var e Email
	var recipientsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, sender, recipients, subject, body, raw_message, received_at FROM emails WHERE id = ?`, id,
	).Scan(&e.ID, &e.Sender, &recipientsJSON, &e.Subject, &e.Body, &e.RawMessage, &e.ReceivedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("email not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("query email: %w", err)
	}
	if err := json.Unmarshal([]byte(recipientsJSON), &e.Recipients); err != nil {
		return nil, fmt.Errorf("unmarshal recipients: %w", err)
	}
	return &e, nil
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
