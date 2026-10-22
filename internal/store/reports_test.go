package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestReportMigrationPreservesSessionsAndReplaysCommittedReport(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "atlas.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.CreateSession(ctx, map[string]any{"state": "answered"})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("DROP TABLE conversation_reports; PRAGMA user_version=11;"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, session.ID); err != nil {
		t.Fatal("migration lost session", err)
	}
	input := ConversationReportInput{RequestID: "report-request-1", SessionID: session.ID, SessionVersion: session.Version, Surface: "web", Category: "wrong_answer", Expected: "Show the saved task", MessageIndex: -1, Snapshot: json.RawMessage(`{"state":"answered"}`)}
	report, replay, err := s.CreateConversationReport(ctx, input)
	if err != nil || replay {
		t.Fatal(report, replay, err)
	}
	input.Expected = "Change the answer"
	if _, _, err = s.CreateConversationReport(ctx, input); !errors.Is(err, ErrReportKeyConflict) {
		t.Fatal("changed request ID was accepted", err)
	}
	input.Expected = "Show the saved task"
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	repeated, replay, err := s.CreateConversationReport(ctx, input)
	if err != nil || !replay || repeated.ID != report.ID {
		t.Fatal(repeated, replay, err)
	}
	all, err := s.ConversationReports(ctx)
	if err != nil || len(all) != 1 {
		t.Fatal(all, err)
	}
}
