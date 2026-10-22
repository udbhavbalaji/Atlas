package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
)

const reportMigration = `CREATE TABLE IF NOT EXISTS conversation_reports(
 id TEXT PRIMARY KEY,
 request_id TEXT NOT NULL UNIQUE,
 request_hash TEXT NOT NULL,
 session_id TEXT NOT NULL,
 session_version INTEGER NOT NULL,
 surface TEXT NOT NULL,
 category TEXT NOT NULL,
 expected TEXT NOT NULL,
 message_index INTEGER NOT NULL,
 snapshot TEXT NOT NULL,
 created_at TEXT NOT NULL
); CREATE INDEX IF NOT EXISTS conversation_reports_created ON conversation_reports(created_at DESC,id DESC); PRAGMA user_version=12;`

var ErrReportInvalid = errors.New("invalid conversation report")
var ErrReportKeyConflict = errors.New("report request ID was already used for different content")

type ConversationReport struct {
	ID             string          `json:"id"`
	RequestID      string          `json:"request_id"`
	SessionID      string          `json:"session_id"`
	SessionVersion int             `json:"session_version"`
	Surface        string          `json:"surface"`
	Category       string          `json:"category"`
	Expected       string          `json:"expected"`
	MessageIndex   int             `json:"message_index"`
	Snapshot       json.RawMessage `json:"snapshot"`
	CreatedAt      string          `json:"created_at"`
}

type ConversationReportInput struct {
	RequestID      string
	SessionID      string
	SessionVersion int
	Surface        string
	Category       string
	Expected       string
	MessageIndex   int
	Snapshot       json.RawMessage
}

func (s *Store) CreateConversationReport(ctx context.Context, input ConversationReportInput) (ConversationReport, bool, error) {
	if input.RequestID == "" || input.SessionID == "" || input.SessionVersion < 1 || !json.Valid(input.Snapshot) {
		return ConversationReport{}, false, ErrReportInvalid
	}
	hash := fingerprint(input.SessionID, input.Surface, input.Category, input.Expected, strconv.Itoa(input.MessageIndex), strconv.Itoa(input.SessionVersion))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ConversationReport{}, false, err
	}
	defer tx.Rollback()
	var existing ConversationReport
	var existingHash, snapshot string
	err = tx.QueryRowContext(ctx, `SELECT id,request_id,request_hash,session_id,session_version,surface,category,expected,message_index,snapshot,created_at
 FROM conversation_reports WHERE request_id=?`, input.RequestID).Scan(&existing.ID, &existing.RequestID, &existingHash, &existing.SessionID, &existing.SessionVersion, &existing.Surface, &existing.Category, &existing.Expected, &existing.MessageIndex, &snapshot, &existing.CreatedAt)
	if err == nil {
		if existingHash != hash {
			return ConversationReport{}, false, ErrReportKeyConflict
		}
		existing.Snapshot = json.RawMessage(snapshot)
		return existing, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ConversationReport{}, false, err
	}
	var currentVersion int
	err = tx.QueryRowContext(ctx, "SELECT version FROM capture_sessions WHERE id=?", input.SessionID).Scan(&currentVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ConversationReport{}, false, ErrSessionNotFound
	}
	if err != nil {
		return ConversationReport{}, false, err
	}
	if currentVersion != input.SessionVersion {
		return ConversationReport{}, false, ErrSessionConflict
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return ConversationReport{}, false, err
	}
	report := ConversationReport{ID: hex.EncodeToString(id[:]), RequestID: input.RequestID, SessionID: input.SessionID, SessionVersion: input.SessionVersion, Surface: input.Surface, Category: input.Category, Expected: input.Expected, MessageIndex: input.MessageIndex, Snapshot: input.Snapshot, CreatedAt: now()}
	_, err = tx.ExecContext(ctx, `INSERT INTO conversation_reports(id,request_id,request_hash,session_id,session_version,surface,category,expected,message_index,snapshot,created_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?)`, report.ID, report.RequestID, hash, report.SessionID, report.SessionVersion, report.Surface, report.Category, report.Expected, report.MessageIndex, string(report.Snapshot), report.CreatedAt)
	if err != nil {
		return ConversationReport{}, false, err
	}
	return report, false, tx.Commit()
}

func (s *Store) ConversationReports(ctx context.Context) ([]ConversationReport, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,request_id,session_id,session_version,surface,category,expected,message_index,snapshot,created_at
 FROM conversation_reports ORDER BY created_at DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reports := []ConversationReport{}
	for rows.Next() {
		var report ConversationReport
		var snapshot string
		if err = rows.Scan(&report.ID, &report.RequestID, &report.SessionID, &report.SessionVersion, &report.Surface, &report.Category, &report.Expected, &report.MessageIndex, &snapshot, &report.CreatedAt); err != nil {
			return nil, err
		}
		report.Snapshot = json.RawMessage(snapshot)
		reports = append(reports, report)
	}
	return reports, rows.Err()
}
