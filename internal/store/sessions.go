package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
)

var ErrSessionNotFound = errors.New("capture session not found")
var ErrSessionConflict = errors.New("capture session changed; reload before replying")

const sessionMigration = `CREATE TABLE capture_sessions(id TEXT PRIMARY KEY,version INTEGER NOT NULL,data TEXT NOT NULL,updated_at TEXT NOT NULL); PRAGMA user_version=10;`

type SessionDocument struct {
	ID        string          `json:"id"`
	Version   int             `json:"version"`
	Data      json.RawMessage `json:"data"`
	UpdatedAt string          `json:"updated_at"`
}

func (s *Store) CreateSession(ctx context.Context, data any) (SessionDocument, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return SessionDocument{}, err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return SessionDocument{}, err
	}
	d := SessionDocument{hex.EncodeToString(b[:]), 1, raw, now()}
	_, err = s.db.ExecContext(ctx, "INSERT INTO capture_sessions VALUES(?,?,?,?)", d.ID, d.Version, string(raw), d.UpdatedAt)
	return d, err
}
func (s *Store) Session(ctx context.Context, id string) (SessionDocument, error) {
	var d SessionDocument
	var raw string
	err := s.db.QueryRowContext(ctx, "SELECT id,version,data,updated_at FROM capture_sessions WHERE id=?", id).Scan(&d.ID, &d.Version, &raw, &d.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrSessionNotFound
	}
	d.Data = json.RawMessage(raw)
	return d, err
}
func (s *Store) SaveSession(ctx context.Context, id string, version int, data any) (SessionDocument, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return SessionDocument{}, err
	}
	d := SessionDocument{id, version + 1, raw, now()}
	result, err := s.db.ExecContext(ctx, "UPDATE capture_sessions SET version=?,data=?,updated_at=? WHERE id=? AND version=?", d.Version, string(raw), d.UpdatedAt, id, version)
	if err != nil {
		return d, err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		err = ErrSessionConflict
	}
	return d, err
}
