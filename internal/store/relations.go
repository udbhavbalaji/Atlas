package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

const relationMigration = `CREATE TABLE record_relations(id TEXT PRIMARY KEY,from_type TEXT NOT NULL CHECK(from_type IN ('task','reminder','note')),from_id TEXT NOT NULL,to_type TEXT NOT NULL CHECK(to_type IN ('task','reminder','note')),to_id TEXT NOT NULL,from_title TEXT NOT NULL,to_title TEXT NOT NULL,created_at TEXT NOT NULL,UNIQUE(from_type,from_id,to_type,to_id)); CREATE INDEX relations_to ON record_relations(to_type,to_id); ALTER TABLE activity ADD COLUMN relation_id TEXT NOT NULL DEFAULT ''; PRAGMA user_version=9;`

var ErrRelation = errors.New("relation requires two distinct task, reminder or note records with valid IDs")

type RelationRecord struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Title  string `json:"title"`
	Exists bool   `json:"exists"`
	Status string `json:"status"`
}
type Relation struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	From      RelationRecord `json:"from"`
	To        RelationRecord `json:"to"`
	CreatedAt string         `json:"created_at"`
}
type RelationAction struct {
	RelationID string    `json:"relation_id"`
	Relation   *Relation `json:"relation"`
	Deleted    bool      `json:"deleted"`
}

func validRelationKind(kind string) bool {
	return kind == "task" || kind == "reminder" || kind == "note"
}
func relationPair(fromKind, fromID, toKind, toID string) (string, string, string, string, string, error) {
	if !validRelationKind(fromKind) || !validRelationKind(toKind) || !taskIDPattern.MatchString(fromID) || !taskIDPattern.MatchString(toID) || (fromKind == toKind && fromID == toID) {
		return "", "", "", "", "", ErrRelation
	}
	if fromKind+":"+fromID > toKind+":"+toID {
		fromKind, toKind = toKind, fromKind
		fromID, toID = toID, fromID
	}
	return fingerprint("relation.related_to.v1", fromKind, fromID, toKind, toID), fromKind, fromID, toKind, toID, nil
}
func relationRecord(ctx context.Context, tx *sql.Tx, kind, id string) (RelationRecord, error) {
	r := RelationRecord{Type: kind, ID: id, Exists: true}
	switch kind {
	case "task":
		t, e := readTask(ctx, tx, id)
		r.Title = t.Title
		r.Status = t.Status
		return r, e
	case "reminder":
		t, e := readReminder(ctx, tx, id)
		r.Title = t.Title
		r.Status = t.Status
		return r, e
	case "note":
		t, e := readNote(ctx, tx, id)
		r.Title = string([]rune(strings.TrimSpace(t.Body)))
		if len([]rune(r.Title)) > 160 {
			r.Title = string([]rune(r.Title)[:160]) + "…"
		}
		r.Status = "stored"
		return r, e
	}
	return r, ErrRelation
}
func readRelations(ctx context.Context, tx *sql.Tx, kind, id string) ([]Relation, error) {
	rows, e := tx.QueryContext(ctx, `WITH records(kind,id,title,status) AS (SELECT 'task',id,title,status FROM tasks UNION ALL SELECT 'reminder',id,title,status FROM reminders UNION ALL SELECT 'note',id,COALESCE(NULLIF(title,''),substr(trim(body),1,160)),'stored' FROM notes) SELECT l.id,l.from_type,l.from_id,COALESCE(a.title,l.from_title),a.id IS NOT NULL,COALESCE(a.status,'missing'),l.to_type,l.to_id,COALESCE(b.title,l.to_title),b.id IS NOT NULL,COALESCE(b.status,'missing'),l.created_at FROM record_relations l LEFT JOIN records a ON a.kind=l.from_type AND a.id=l.from_id LEFT JOIN records b ON b.kind=l.to_type AND b.id=l.to_id WHERE ?='' OR (l.from_type=? AND l.from_id=?) OR (l.to_type=? AND l.to_id=?) ORDER BY l.created_at DESC,l.id`, kind, kind, id, kind, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	list := []Relation{}
	for rows.Next() {
		r := Relation{Type: "related_to"}
		if e = rows.Scan(&r.ID, &r.From.Type, &r.From.ID, &r.From.Title, &r.From.Exists, &r.From.Status, &r.To.Type, &r.To.ID, &r.To.Title, &r.To.Exists, &r.To.Status, &r.CreatedAt); e != nil {
			return nil, e
		}
		list = append(list, r)
	}
	return list, rows.Err()
}
func (s *Store) Relations(ctx context.Context, kind, id string) ([]Relation, error) {
	if (kind != "" || id != "") && (!validRelationKind(kind) || !taskIDPattern.MatchString(id)) {
		return nil, ErrRelation
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	list, e := readRelations(ctx, tx, kind, id)
	if e == nil {
		e = tx.Commit()
	}
	return list, e
}
func (s *Store) SetRelation(ctx context.Context, fromKind, fromID, toKind, toID string, attach bool) (RelationAction, error) {
	id, fk, fi, tk, ti, e := relationPair(fromKind, fromID, toKind, toID)
	action := RelationAction{RelationID: id, Deleted: !attach}
	if e != nil {
		return action, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return action, e
	}
	defer tx.Rollback()
	var result sql.Result
	if attach {
		from, e := relationRecord(ctx, tx, fk, fi)
		if e != nil {
			return action, e
		}
		to, e := relationRecord(ctx, tx, tk, ti)
		if e != nil {
			return action, e
		}
		result, e = tx.ExecContext(ctx, "INSERT INTO record_relations VALUES(?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING", id, fk, fi, tk, ti, from.Title, to.Title, now())
		if e != nil {
			return action, e
		}
	} else {
		result, e = tx.ExecContext(ctx, "DELETE FROM record_relations WHERE id=?", id)
		if e != nil {
			return action, e
		}
	}
	count, e := result.RowsAffected()
	if e != nil {
		return action, e
	}
	if count > 0 {
		name := "relation.added"
		if !attach {
			name = "relation.removed"
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO activity(task_id,action,timestamp,relation_id) VALUES('',?,?,?)", name, now(), id)
		if e != nil {
			return action, e
		}
	}
	if attach {
		list, e := readRelations(ctx, tx, fk, fi)
		if e != nil {
			return action, e
		}
		for _, r := range list {
			if r.ID == id {
				copy := r
				action.Relation = &copy
				break
			}
		}
		if action.Relation == nil {
			return action, ErrRelation
		}
	}
	e = tx.Commit()
	return action, e
}
