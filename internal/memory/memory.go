package memory

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/YoungsoonLee/meowclaw/internal/agent/provider"
)

type Store struct {
	db *sql.DB
}

func New(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	slog.Info("memory store initialized", "path", dbPath)
	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	coreSchema := `
	CREATE TABLE IF NOT EXISTS messages (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id  TEXT NOT NULL,
		role        TEXT NOT NULL,
		content     TEXT NOT NULL,
		tokens      TEXT DEFAULT '',
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id);
	CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at);

	CREATE TABLE IF NOT EXISTS sessions (
		id         TEXT PRIMARY KEY,
		channel    TEXT DEFAULT '',
		metadata   TEXT DEFAULT '{}',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.Exec(coreSchema); err != nil {
		return err
	}

	// FTS5 is optional; fall back to LIKE-based search if unavailable
	ftsSchema := `
	CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
		content,
		session_id UNINDEXED,
		role UNINDEXED,
		content=messages,
		content_rowid=id
	);

	CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
		INSERT INTO messages_fts(rowid, content, session_id, role)
		VALUES (new.id, new.content, new.session_id, new.role);
	END;

	CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
		INSERT INTO messages_fts(messages_fts, rowid, content, session_id, role)
		VALUES ('delete', old.id, old.content, old.session_id, old.role);
	END;
	`
	if _, err := db.Exec(ftsSchema); err != nil {
		slog.Warn("FTS5 not available, using LIKE-based search fallback", "error", err)
	}
	return nil
}

func (s *Store) Save(ctx context.Context, sessionID, role, content string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO sessions (id, updated_at) VALUES (?, CURRENT_TIMESTAMP)`,
		sessionID,
	)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		sessionID,
	)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO messages (session_id, role, content) VALUES (?, ?, ?)`,
		sessionID, role, content,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) LoadHistory(ctx context.Context, sessionID string, limit int) ([]provider.ChatMessage, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT role, content FROM messages WHERE session_id = ? ORDER BY id DESC LIMIT ?`,
		sessionID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []provider.ChatMessage
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			return nil, err
		}
		msgs = append(msgs, provider.ChatMessage{
			Role:    provider.Role(role),
			Content: content,
		})
	}

	// reverse to chronological order
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}

	return msgs, rows.Err()
}

// Search uses SQLite FTS5 for full-text search, falling back to LIKE.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 10
	}

	// Try FTS5 first
	query = strings.ReplaceAll(query, `"`, `""`)
	rows, err := s.db.QueryContext(ctx,
		`SELECT content FROM messages_fts WHERE messages_fts MATCH ? ORDER BY rank LIMIT ?`,
		`"`+query+`"`, limit,
	)
	if err != nil {
		// Fallback to LIKE-based search
		rows, err = s.db.QueryContext(ctx,
			`SELECT content FROM messages WHERE content LIKE ? ORDER BY created_at DESC LIMIT ?`,
			"%"+query+"%", limit,
		)
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, err
		}
		results = append(results, content)
	}

	return results, rows.Err()
}

type SessionInfo struct {
	ID        string    `json:"id"`
	Channel   string    `json:"channel"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	MsgCount  int       `json:"message_count"`
}

func (s *Store) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.channel, s.created_at, s.updated_at,
		       (SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id) as msg_count
		FROM sessions s ORDER BY s.updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []SessionInfo
	for rows.Next() {
		var si SessionInfo
		if err := rows.Scan(&si.ID, &si.Channel, &si.CreatedAt, &si.UpdatedAt, &si.MsgCount); err != nil {
			return nil, err
		}
		sessions = append(sessions, si)
	}
	return sessions, rows.Err()
}

func (s *Store) Close() error {
	return s.db.Close()
}
