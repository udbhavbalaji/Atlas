package httpapi

import (
	"atlas/internal/store"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"reflect"
	"strings"
	"unicode/utf8"
)

type ErrorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

func apiError(w http.ResponseWriter, status int, code, message string, retryable bool) {
	respond(w, status, ErrorResponse{Error: ErrorDetail{code, message, retryable}})
}
func failure(w http.ResponseWriter, e error) {
	status, code, retryable := 500, "internal_error", false
	switch {
	case errors.Is(e, store.ErrRelation):
		status, code = 400, "invalid_relation"
	case errors.Is(e, store.ErrDependencyCycle):
		status, code = 409, "dependency_cycle"
	case errors.Is(e, store.ErrDependency):
		status, code = 400, "invalid_dependency"
	case errors.Is(e, store.ErrCaptureContext):
		status, code = 409, "capture_context_changed"
	case errors.Is(e, store.ErrCaptureKind):
		status, code = 400, "invalid_capture_kind"
	case errors.Is(e, store.ErrCapturePreview):
		status, code = 409, "capture_preview_conflict"
	case errors.Is(e, store.ErrCaptureKey):
		status, code = 400, "capture_key_required"
	case errors.Is(e, store.ErrCaptureReminder):
		status, code = 400, "invalid_capture_reminder"
	case errors.Is(e, store.ErrInvalidRepeat):
		status, code = 400, "invalid_repeat"
	case errors.Is(e, store.ErrOccurrenceConflict):
		status, code = 409, "occurrence_state_conflict"
	case errors.Is(e, store.ErrInvalidNote):
		status, code = 400, "invalid_note"
	case errors.Is(e, store.ErrInvalidNoteLink):
		status, code = 400, "invalid_note_link"
	case errors.Is(e, store.ErrNoteNotFound):
		status, code = 404, "note_not_found"
	case errors.Is(e, store.ErrInvalidSearch):
		status, code = 400, "invalid_search"
	case errors.Is(e, store.ErrInvalid):
		status, code = 400, "invalid_title"
	case errors.Is(e, store.ErrInvalidUpdate):
		status, code = 400, "invalid_task_patch"
	case errors.Is(e, store.ErrInvalidFields):
		status, code = 400, "invalid_task_fields"
	case errors.Is(e, store.ErrInvalidReminder):
		status, code = 400, "invalid_reminder"
	case errors.Is(e, store.ErrSnoozeTime):
		status, code = 400, "invalid_snooze_time"
	case errors.Is(e, store.ErrInvalidKey):
		status, code = 400, "invalid_idempotency_key"
	case errors.Is(e, store.ErrNotFound):
		status, code = 404, "task_not_found"
	case errors.Is(e, store.ErrReminderNotFound):
		status, code = 404, "reminder_not_found"
	case errors.Is(e, store.ErrReminderConflict):
		status, code = 409, "reminder_state_conflict"
	case errors.Is(e, store.ErrTaskReminderConflict):
		status, code = 409, "task_not_open"
	case errors.Is(e, store.ErrIdempotencyConflict):
		status, code = 409, "idempotency_conflict"
	case errors.Is(e, context.DeadlineExceeded), errors.Is(e, context.Canceled):
		status, code, retryable = 503, "request_interrupted", true
	}
	var sqliteError interface{ Code() int }
	if errors.As(e, &sqliteError) && (sqliteError.Code()&255 == 5 || sqliteError.Code()&255 == 6) {
		status, code, retryable = 503, "storage_busy", true
		w.Header().Set("Retry-After", "1")
	}
	message := e.Error()
	if status == 500 || status == 503 {
		log.Printf("API storage operation failed: %v", e)
		message = "Atlas could not finish this operation."
	}
	apiError(w, status, code, message, retryable)
}
func creationKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	values, present := r.Header["Idempotency-Key"]
	if !present {
		return "", true
	}
	if len(values) != 1 || values[0] == "" || !store.ValidKey(values[0]) {
		failure(w, store.ErrInvalidKey)
		return "", false
	}
	return values[0], true
}
func creationResponse(w http.ResponseWriter, key string, replay bool, location string, v any) {
	w.Header().Set("Location", location)
	if key != "" {
		w.Header().Set("Idempotency-Key", key)
	}
	status := 201
	w.Header().Set("Idempotency-Replayed", "false")
	if replay {
		status = 200
		w.Header().Set("Idempotency-Replayed", "true")
	}
	respond(w, status, v)
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool { return decodeBody(w, r, v, false) }
func emptyAction(w http.ResponseWriter, r *http.Request) bool {
	return decodeBody(w, r, &struct{}{}, true)
}
func decodeBody(w http.ResponseWriter, r *http.Request, v any, allowEmpty bool) bool {
	if r.Body == nil {
		r.Body = http.NoBody
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
	if err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			apiError(w, 413, "body_too_large", "Request body exceeds 65536 bytes.", false)
		} else {
			apiError(w, 400, "invalid_json", "Could not read request body.", false)
		}
		return false
	}
	if allowEmpty && len(bytes.TrimSpace(data)) == 0 {
		return true
	}
	if !utf8.Valid(data) {
		apiError(w, 400, "invalid_json", "Request body must be valid UTF-8.", false)
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		apiError(w, 400, "invalid_json", "Expected one JSON object.", false)
		return false
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		token, e := d.Token()
		key, ok := token.(string)
		if e != nil || !ok {
			apiError(w, 400, "invalid_json", "Invalid JSON object.", false)
			return false
		}
		if _, exists := fields[key]; exists {
			apiError(w, 400, "duplicate_field", "Duplicate JSON fields are not allowed.", false)
			return false
		}
		var value json.RawMessage
		if e = d.Decode(&value); e != nil {
			apiError(w, 400, "invalid_json", "Invalid JSON value.", false)
			return false
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			apiError(w, 400, "null_field", "Null fields are not allowed; omit a field or use an empty string to clear it.", false)
			return false
		}
		fields[key] = value
	}
	if _, err = d.Token(); err != nil {
		apiError(w, 400, "invalid_json", "Invalid JSON object.", false)
		return false
	}
	if err = d.Decode(new(any)); err != io.EOF {
		apiError(w, 400, "invalid_json", "Expected one JSON object.", false)
		return false
	}
	allowed := map[string]bool{}
	typ := reflect.TypeOf(v).Elem()
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			allowed[name] = true
		}
	}
	for name := range fields {
		if !allowed[name] {
			apiError(w, 400, "invalid_fields", "Request contains unknown fields or incorrect field types.", false)
			return false
		}
	}
	normalized, _ := json.Marshal(fields)
	strict := json.NewDecoder(bytes.NewReader(normalized))
	strict.DisallowUnknownFields()
	if err = strict.Decode(v); err != nil {
		apiError(w, 400, "invalid_fields", "Request contains unknown fields or incorrect field types.", false)
		return false
	}
	return true
}
