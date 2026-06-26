package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// Contact represents the contacts table record.
type Contact struct {
	PhoneNumber string
	Name        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ChatSession represents the chat_sessions table record.
type ChatSession struct {
	ID                  string
	CustomerPhoneNumber string
	CurrentHandler      string // "bot" or "human"
	SessionStatus       string // "open" or "closed"
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// ChatSessionDetail contains a session's details along with contact name and messages.
type ChatSessionDetail struct {
	ID                  string
	CustomerPhoneNumber string
	CustomerName        string
	CurrentHandler      string
	SessionStatus       string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Messages            []*Message
}

// Message represents the messages table record.
type Message struct {
	ID                   string
	SessionID            string
	SenderPhoneNumber    string
	RecipientPhoneNumber string
	Body                 string
	Timestamp            time.Time
}

// Store defines database operations for the WhatsApp CS system.
type Store interface {
	GetOrCreateContact(ctx context.Context, phoneNumber, name string) (*Contact, error)
	GetActiveSession(ctx context.Context, customerPhoneNumber string) (*ChatSession, error)
	CreateSession(ctx context.Context, customerPhoneNumber string, currentHandler, status string) (*ChatSession, error)
	UpdateSessionHandler(ctx context.Context, sessionID string, handler string) error
	UpdateSessionStatus(ctx context.Context, sessionID string, status string) error
	LogMessage(ctx context.Context, sessionID string, sender string, recipient string, body string) error
	GetChatSessions(ctx context.Context) ([]*ChatSessionDetail, error)
	GetSessionMessages(ctx context.Context, sessionID string) ([]*Message, error)
	Close() error
}

// SQLStore implements Store interface using database/sql and SQLite.
type SQLStore struct {
	db *sql.DB
}

// NewSQLStore creates and initializes SQLStore, applying schema migrations.
func NewSQLStore(dbPath string) (*SQLStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	store := &SQLStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return store, nil
}

// Close closes the underlying database connection.
func (s *SQLStore) Close() error {
	return s.db.Close()
}

func (s *SQLStore) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS contacts (
			phone_number TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			customer_phone_number TEXT NOT NULL,
			current_handler TEXT NOT NULL CHECK(current_handler IN ('bot', 'human')),
			session_status TEXT NOT NULL CHECK(session_status IN ('open', 'closed')),
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(customer_phone_number) REFERENCES contacts(phone_number) ON UPDATE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			sender_phone_number TEXT NOT NULL,
			recipient_phone_number TEXT NOT NULL,
			body TEXT NOT NULL,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE
		);`,
		// Indexes for fast lookups
		`CREATE INDEX IF NOT EXISTS idx_contacts_phone_number ON contacts(phone_number);`,
		`CREATE INDEX IF NOT EXISTS idx_chat_sessions_customer_status ON chat_sessions(customer_phone_number, session_status);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_session_timestamp ON messages(session_id, timestamp);`,
	}

	for _, query := range queries {
		if _, err := s.db.Exec(query); err != nil {
			return fmt.Errorf("migration query failed: %w (query: %s)", err, query)
		}
	}
	return nil
}

// GetOrCreateContact checks if contact exists. If not, it creates it.
func (s *SQLStore) GetOrCreateContact(ctx context.Context, phoneNumber, name string) (*Contact, error) {
	// Attempt to insert. If conflict, update name if needed.
	query := `
		INSERT INTO contacts (phone_number, name, created_at, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(phone_number) DO UPDATE SET
			name = excluded.name,
			updated_at = CURRENT_TIMESTAMP
		RETURNING phone_number, name, created_at, updated_at;
	`
	var c Contact
	err := s.db.QueryRowContext(ctx, query, phoneNumber, name).Scan(
		&c.PhoneNumber, &c.Name, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get or create contact: %w", err)
	}
	return &c, nil
}

// GetActiveSession finds an open session for the customer.
func (s *SQLStore) GetActiveSession(ctx context.Context, customerPhoneNumber string) (*ChatSession, error) {
	query := `
		SELECT id, customer_phone_number, current_handler, session_status, created_at, updated_at
		FROM chat_sessions
		WHERE customer_phone_number = ? AND session_status = 'open'
		LIMIT 1;
	`
	var session ChatSession
	err := s.db.QueryRowContext(ctx, query, customerPhoneNumber).Scan(
		&session.ID, &session.CustomerPhoneNumber, &session.CurrentHandler,
		&session.SessionStatus, &session.CreatedAt, &session.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get active session: %w", err)
	}
	return &session, nil
}

// CreateSession initializes a new chat session.
func (s *SQLStore) CreateSession(ctx context.Context, customerPhoneNumber string, currentHandler, status string) (*ChatSession, error) {
	id := uuid.New().String()
	query := `
		INSERT INTO chat_sessions (id, customer_phone_number, current_handler, session_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING id, customer_phone_number, current_handler, session_status, created_at, updated_at;
	`
	var session ChatSession
	err := s.db.QueryRowContext(ctx, query, id, customerPhoneNumber, currentHandler, status).Scan(
		&session.ID, &session.CustomerPhoneNumber, &session.CurrentHandler,
		&session.SessionStatus, &session.CreatedAt, &session.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	return &session, nil
}

// UpdateSessionHandler changes current_handler ("bot" to "human" or vice versa).
func (s *SQLStore) UpdateSessionHandler(ctx context.Context, sessionID string, handler string) error {
	query := `
		UPDATE chat_sessions
		SET current_handler = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?;
	`
	_, err := s.db.ExecContext(ctx, query, handler, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session handler: %w", err)
	}
	return nil
}

// UpdateSessionStatus changes session status ("open" or "closed").
func (s *SQLStore) UpdateSessionStatus(ctx context.Context, sessionID string, status string) error {
	query := `
		UPDATE chat_sessions
		SET session_status = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?;
	`
	_, err := s.db.ExecContext(ctx, query, status, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session status: %w", err)
	}
	return nil
}

// LogMessage inserts a message into logs.
func (s *SQLStore) LogMessage(ctx context.Context, sessionID string, sender string, recipient string, body string) error {
	id := uuid.New().String()
	query := `
		INSERT INTO messages (id, session_id, sender_phone_number, recipient_phone_number, body, timestamp)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP);
	`
	_, err := s.db.ExecContext(ctx, query, id, sessionID, sender, recipient, body)
	if err != nil {
		return fmt.Errorf("failed to log message: %w", err)
	}
	return nil
}

// GetChatSessions queries all chat sessions joined with contact names, sorted by last update.
func (s *SQLStore) GetChatSessions(ctx context.Context) ([]*ChatSessionDetail, error) {
	query := `
		SELECT s.id, s.customer_phone_number, c.name, s.current_handler, s.session_status, s.created_at, s.updated_at
		FROM chat_sessions s
		JOIN contacts c ON s.customer_phone_number = c.phone_number
		ORDER BY s.updated_at DESC;
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query chat sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*ChatSessionDetail
	for rows.Next() {
		var sd ChatSessionDetail
		err := rows.Scan(&sd.ID, &sd.CustomerPhoneNumber, &sd.CustomerName, &sd.CurrentHandler, &sd.SessionStatus, &sd.CreatedAt, &sd.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chat session detail: %w", err)
		}
		sessions = append(sessions, &sd)
	}

	for _, sd := range sessions {
		msgs, err := s.GetSessionMessages(ctx, sd.ID)
		if err != nil {
			return nil, err
		}
		sd.Messages = msgs
	}

	return sessions, nil
}

// GetSessionMessages fetches all messages recorded in a chat session.
func (s *SQLStore) GetSessionMessages(ctx context.Context, sessionID string) ([]*Message, error) {
	query := `
		SELECT id, session_id, sender_phone_number, recipient_phone_number, body, timestamp
		FROM messages
		WHERE session_id = ?
		ORDER BY timestamp ASC;
	`
	rows, err := s.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	var messages []*Message
	for rows.Next() {
		var m Message
		err := rows.Scan(&m.ID, &m.SessionID, &m.SenderPhoneNumber, &m.RecipientPhoneNumber, &m.Body, &m.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		messages = append(messages, &m)
	}
	return messages, nil
}
