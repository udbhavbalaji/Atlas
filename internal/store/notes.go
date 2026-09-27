package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"
)

const noteMigration = `CREATE TABLE notes(id TEXT PRIMARY KEY,body TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL); CREATE TABLE note_links(note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,target_type TEXT NOT NULL CHECK(target_type IN ('task','reminder')),target_id TEXT NOT NULL,target_title TEXT NOT NULL,PRIMARY KEY(note_id,target_type,target_id)); CREATE INDEX notes_by_target ON note_links(target_type,target_id); ALTER TABLE activity ADD COLUMN note_id TEXT NOT NULL DEFAULT ''; PRAGMA user_version=7;`

var ErrInvalidNote = errors.New("note body must contain non-whitespace text and be at most 10000 Unicode code points")
var ErrNoteNotFound = errors.New("note not found")
var ErrInvalidNoteLink = errors.New("note link type must be task or reminder, with a nonempty target ID")

type NoteLink struct {
	TargetType   string `json:"target_type"`
	TargetID     string `json:"target_id"`
	TargetTitle  string `json:"target_title"`
	TargetExists bool   `json:"target_exists"`
}
type Note struct {
	ID        string     `json:"id"`
	Body      string     `json:"body"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
	Links     []NoteLink `json:"links"`
}
type NoteAction struct {
	NoteID  string `json:"note_id"`
	Note    *Note  `json:"note"`
	Deleted bool   `json:"deleted"`
}

func validateNote(body string) error {
	if !utf8.ValidString(body) || strings.TrimSpace(body) == "" || len([]rune(body)) > 10000 {
		return ErrInvalidNote
	}
	return nil
}
func noteActivity(ctx context.Context, tx *sql.Tx, id, kind, target, action, at string) error {
	task, reminder := "", ""
	if kind == "task" {
		task = target
	}
	if kind == "reminder" {
		reminder = target
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO activity(task_id,reminder_id,note_id,action,timestamp) VALUES(?,?,?,?,?)", task, reminder, id, action, at)
	return err
}
func noteTarget(ctx context.Context, tx *sql.Tx, kind, id string) (string, error) {
	if id == "" {
		return "", ErrInvalidNoteLink
	}
	switch kind {
	case "task":
		t, e := readTask(ctx, tx, id)
		return t.Title, e
	case "reminder":
		r, e := readReminder(ctx, tx, id)
		return r.Title, e
	default:
		return "", ErrInvalidNoteLink
	}
}
func readNote(ctx context.Context, tx *sql.Tx, id string) (Note, error) {
	var n Note
	n.Links = []NoteLink{}
	err := tx.QueryRowContext(ctx, "SELECT id,body,created_at,updated_at FROM notes WHERE id=?", id).Scan(&n.ID, &n.Body, &n.CreatedAt, &n.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNoteNotFound
	}
	if err != nil {
		return n, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT l.target_type,l.target_id,COALESCE(t.title,r.title,l.target_title),CASE WHEN t.id IS NOT NULL OR r.id IS NOT NULL THEN 1 ELSE 0 END FROM note_links l LEFT JOIN tasks t ON l.target_type='task' AND t.id=l.target_id LEFT JOIN reminders r ON l.target_type='reminder' AND r.id=l.target_id WHERE l.note_id=? ORDER BY l.target_type,l.target_id`, id)
	if err != nil {
		return n, err
	}
	defer rows.Close()
	for rows.Next() {
		var link NoteLink
		if err = rows.Scan(&link.TargetType, &link.TargetID, &link.TargetTitle, &link.TargetExists); err != nil {
			return n, err
		}
		n.Links = append(n.Links, link)
	}
	return n, rows.Err()
}
func notesInTransaction(ctx context.Context, tx *sql.Tx, kind, target string) ([]Note, error) {
	query := "SELECT id FROM notes ORDER BY updated_at DESC,id"
	args := []any{}
	if kind != "" {
		query = "SELECT n.id FROM notes n JOIN note_links l ON l.note_id=n.id WHERE l.target_type=? AND l.target_id=? ORDER BY n.updated_at DESC,n.id"
		args = []any{kind, target}
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	notes := []Note{}
	for _, id := range ids {
		n, e := readNote(ctx, tx, id)
		if e != nil {
			return nil, e
		}
		notes = append(notes, n)
	}
	return notes, nil
}
func (s *Store) Notes(ctx context.Context) ([]Note, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	notes, e := notesInTransaction(ctx, tx, "", "")
	if e == nil {
		e = tx.Commit()
	}
	return notes, e
}
func (s *Store) NoteState(ctx context.Context, id string) (NoteAction, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return NoteAction{}, e
	}
	defer tx.Rollback()
	n, e := readNote(ctx, tx, id)
	v := NoteAction{NoteID: id, Note: &n}
	if e == nil {
		e = tx.Commit()
	}
	return v, e
}
func (s *Store) CreateNoteRequest(ctx context.Context, key, body, taskID, reminderID string) (NoteAction, bool, error) {
	if e := validateNote(body); e != nil {
		return NoteAction{}, false, e
	}
	if !ValidKey(key) {
		return NoteAction{}, false, ErrInvalidKey
	}
	hash := fingerprint(body, taskID, reminderID)
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return NoteAction{}, false, e
	}
	defer tx.Rollback()
	var saved NoteAction
	replay, e := replayReceipt(ctx, tx, "notes.create", key, hash, &saved)
	if e != nil {
		return saved, false, e
	}
	if replay {
		return saved, true, tx.Commit()
	}
	id, e := newID()
	if e != nil {
		return saved, false, e
	}
	at := now()
	_, e = tx.ExecContext(ctx, "INSERT INTO notes VALUES(?,?,?,?)", id, body, at, at)
	if e != nil {
		return saved, false, e
	}
	for _, target := range []struct{ kind, id string }{{"task", taskID}, {"reminder", reminderID}} {
		if target.id != "" {
			title, err := noteTarget(ctx, tx, target.kind, target.id)
			if err != nil {
				return saved, false, err
			}
			_, e = tx.ExecContext(ctx, "INSERT INTO note_links VALUES(?,?,?,?)", id, target.kind, target.id, title)
			if e == nil {
				e = noteActivity(ctx, tx, id, target.kind, target.id, "note.linked", at)
			}
			if e != nil {
				return saved, false, e
			}
		}
	}
	if e = noteActivity(ctx, tx, id, "", "", "note.created", at); e != nil {
		return saved, false, e
	}
	n, e := readNote(ctx, tx, id)
	if e != nil {
		return saved, false, e
	}
	v := NoteAction{NoteID: id, Note: &n}
	if e = saveReceipt(ctx, tx, "notes.create", key, hash, v); e == nil {
		e = tx.Commit()
	}
	return v, false, e
}
func (s *Store) UpdateNote(ctx context.Context, id, body string) (NoteAction, error) {
	if e := validateNote(body); e != nil {
		return NoteAction{}, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return NoteAction{}, e
	}
	defer tx.Rollback()
	n, e := readNote(ctx, tx, id)
	if e != nil {
		return NoteAction{}, e
	}
	if n.Body != body {
		n.Body = body
		n.UpdatedAt = now()
		_, e = tx.ExecContext(ctx, "UPDATE notes SET body=?,updated_at=? WHERE id=?", body, n.UpdatedAt, id)
		if e == nil {
			e = noteActivity(ctx, tx, id, "", "", "note.updated", n.UpdatedAt)
		}
	}
	v := NoteAction{NoteID: id, Note: &n}
	if e == nil {
		e = tx.Commit()
	}
	return v, e
}
func (s *Store) DeleteNote(ctx context.Context, id string) (NoteAction, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return NoteAction{}, e
	}
	defer tx.Rollback()
	if _, e = readNote(ctx, tx, id); e != nil {
		return NoteAction{}, e
	}
	_, e = tx.ExecContext(ctx, "DELETE FROM notes WHERE id=?", id)
	if e == nil {
		e = noteActivity(ctx, tx, id, "", "", "note.deleted", now())
	}
	v := NoteAction{NoteID: id, Deleted: true}
	if e == nil {
		e = tx.Commit()
	}
	return v, e
}
func (s *Store) SetNoteLink(ctx context.Context, id, kind, target string, attach bool) (NoteAction, error) {
	if (kind != "task" && kind != "reminder") || target == "" {
		return NoteAction{}, ErrInvalidNoteLink
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return NoteAction{}, e
	}
	defer tx.Rollback()
	if _, e = readNote(ctx, tx, id); e != nil {
		return NoteAction{}, e
	}
	var result sql.Result
	if attach {
		title, err := noteTarget(ctx, tx, kind, target)
		if err != nil {
			return NoteAction{}, err
		}
		result, e = tx.ExecContext(ctx, "INSERT INTO note_links VALUES(?,?,?,?) ON CONFLICT DO NOTHING", id, kind, target, title)
	} else {
		result, e = tx.ExecContext(ctx, "DELETE FROM note_links WHERE note_id=? AND target_type=? AND target_id=?", id, kind, target)
	}
	if e != nil {
		return NoteAction{}, e
	}
	changed, e := result.RowsAffected()
	if e != nil {
		return NoteAction{}, e
	}
	if changed > 0 {
		at := now()
		_, e = tx.ExecContext(ctx, "UPDATE notes SET updated_at=? WHERE id=?", at, id)
		action := "note.unlinked"
		if attach {
			action = "note.linked"
		}
		if e == nil {
			e = noteActivity(ctx, tx, id, kind, target, action, at)
		}
	}
	if e != nil {
		return NoteAction{}, e
	}
	n, e := readNote(ctx, tx, id)
	v := NoteAction{NoteID: id, Note: &n}
	if e == nil {
		e = tx.Commit()
	}
	return v, e
}
