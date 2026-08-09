// Package store persists VNC endpoints in SQLite (pure-Go driver, no CGO).
package store

import (
	"context"
	"database/sql"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// Endpoint is a VNC target. Agent is the reverse-tunnel agent id to route
// through, or "" for a direct dial from the gateway.
type Endpoint struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Addr        string `json:"addr"`
	Agent       string `json:"agent"`
	Description string `json:"description"`
	PasswordEnc string `json:"-"`
	HasPassword bool   `json:"hasPassword"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// Store is the endpoint repository.
type Store struct{ db *sql.DB }

// Open opens (and migrates) the SQLite database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: single writer keeps it simple
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS vnc_endpoints (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  name         TEXT    NOT NULL DEFAULT '',
  addr         TEXT    NOT NULL,
  agent        TEXT    NOT NULL DEFAULT '',
  description  TEXT    NOT NULL DEFAULT '',
  password_enc TEXT    NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
)`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func scan(row interface{ Scan(...any) error }) (*Endpoint, error) {
	var e Endpoint
	if err := row.Scan(&e.ID, &e.Name, &e.Addr, &e.Agent, &e.Description,
		&e.PasswordEnc, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return nil, err
	}
	e.HasPassword = e.PasswordEnc != ""
	return &e, nil
}

const cols = "id, name, addr, agent, description, password_enc, created_at, updated_at"

// List returns all endpoints ordered by id.
func (s *Store) List(ctx context.Context) ([]*Endpoint, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+cols+" FROM vnc_endpoints ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Endpoint{}
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Get returns one endpoint, or (nil, nil) if not found.
func (s *Store) Get(ctx context.Context, id int64) (*Endpoint, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+cols+" FROM vnc_endpoints WHERE id = ?", id)
	e, err := scan(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return e, err
}

// Create inserts e and returns its new id.
func (s *Store) Create(ctx context.Context, e *Endpoint) (int64, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO vnc_endpoints (name, addr, agent, description, password_enc, created_at, updated_at) VALUES (?,?,?,?,?,?,?)",
		e.Name, e.Addr, e.Agent, e.Description, e.PasswordEnc, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Update writes all mutable fields of e.
func (s *Store) Update(ctx context.Context, e *Endpoint) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE vnc_endpoints SET name=?, addr=?, agent=?, description=?, password_enc=?, updated_at=? WHERE id=?",
		e.Name, e.Addr, e.Agent, e.Description, e.PasswordEnc, time.Now().Unix(), e.ID)
	return err
}

// Delete removes an endpoint.
func (s *Store) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM vnc_endpoints WHERE id = ?", id)
	return err
}
