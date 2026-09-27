# Atlas HTTP API

The local API uses `/api/v1` and JSON. Machine-readable schemas are served at `/openapi.json` (OpenAPI 3.1). API responses include `Atlas-API-Contract: 2` and `Cache-Control: no-store`. Keep the service on loopback; authentication is not implemented.

## Contract revision 2

This development revision changes errors from a string to a structured object, task PATCH from a task to a task-state object, and action responses from empty 204 responses to structured 200 responses. The bundled webpage is updated. Existing external clients must update their response parsing.

| Operation | Response |
| --- | --- |
| POST tasks / reminders | 201 record; keyed replay returns 200 original record |
| GET tasks / reminders | 200 array, empty results `[]` |
| GET tasks/{id} | 200 task state |
| PATCH tasks/{id} | 200 task state |
| POST tasks/{id}/complete | 200 task state |
| DELETE tasks/{id} | 200 task state, `deleted: true`, `task: null` |
| GET reminders/{id} | 200 reminder state |
| POST reminders/{id}/snooze, /dismiss, /complete | 200 reminder state |
| GET deliveries / activity | 200 array of latest 100 records |

Paths above are relative to `/api/v1`. `GET /healthz` returns `{"status":"ok"}`.

Task state contains `task_id`, `task`, `deleted`, `reminders`, and `deliveries`. It includes all linked reminder records and their delivery history, including cancelled records. Reminder state contains `reminder`, `task`, and `deliveries`; `task` is null for standalone reminders or a deleted linked task. Empty collections are always arrays. These snapshots are read within the same transaction as the mutation, then returned after successful commit. Scheduler ticks or subsequent requests can change state afterward; GET the resource again for fresh state.

Creation responses remain the created record. Use its `Location` header to fetch the full state.

## Creation retries

POST tasks and POST reminders accept an optional `Idempotency-Key`, containing 1–128 ASCII letters, digits, dots, underscores, or hyphens. Generate one key per logical creation and reuse it when retrying uncertain results.

- First success: 201, `Idempotency-Replayed: false`.
- Same key and parsed input: 200, `Idempotency-Replayed: true`, original creation response.
- Same key with different valid input: 409 `idempotency_conflict`.
- Without a key: each successful call creates a new record.

Keys are scoped independently to task creation and reminder creation. Receipts, records, initial deliveries, and activity commit atomically. Receipts survive restart and are retained without automatic expiry, including after deletion. Replay does not resurrect a deleted task or restore an old reminder state. It returns the original response; follow `Location` for current state (possibly 404). JSON formatting and property order do not affect matching; string values do, even when normalization produces the same stored value. Omitted optional strings and empty strings match. Invalid input is validated before replay.

The webpage retains a creation key after a failed request for the same input, until confirmed success. These keys are held in page memory; they do not survive reload. There is no offline request queue. Snooze and other mutations do not use creation receipts. Repeated completion/dismissal is a no-op when already in that state; repeated deletion returns 404.

## Input and errors

Requests accept one UTF-8 JSON object, at most 65,536 bytes. Unknown or incorrectly cased fields, wrong types, duplicates, nulls, and trailing JSON are rejected. Completion, dismissal, and deletion accept an empty body or `{}`. PATCH requires at least one supported field; omitted fields stay unchanged and empty strings clear details/deadlines.

IDs are random 128-bit hex strings. Timestamps include explicit offsets and are stored as UTC RFC3339. Titles are trimmed and limited to 500 Unicode code points; details to 10,000. Reminder creation requires `title`, `scheduled_at`, and an IANA `timezone`; optional `task_id` must refer to an open task. Snooze requires a future `scheduled_at`.

```json
{"error":{"code":"idempotency_conflict","message":"idempotency key was already used with different input","retryable":false}}
```

Clients should branch on `error.code`, not message text. Validation errors return 400 (oversize bodies 413), missing resources 404, unsupported methods 405 with `Allow`, and state/key conflicts 409. Transient storage locking returns 503 `storage_busy`, `retryable: true`, and `Retry-After: 1`; interrupted operations return 503 `request_interrupted`. Other internal failures return 500 with a generic message. Retry an uncertain creation with its original key. The OpenAPI schema lists the stable error codes.

## Manual test

Open the **API lab tab** on the homepage. It uses real data on that server.

1. Choose **Create task**, send the default body, and inspect 201, ID, and Location.
2. Click **Replay last request**. Expect 200, `replayed: true`, the same ID, and one task.
3. Change the title and send with the same key. Expect 409 `idempotency_conflict`.
4. Choose **Complete task** (record ID is retained). Expect task status `completed`, plus reminder/delivery arrays.
5. Create a linked reminder on another open task; complete that task and inspect the cancelled reminder and delivery in the response.
6. Send malformed input to inspect validation errors. Use **New request key** for a distinct creation.

Normal webpage actions also display their latest mutation response in the panel. Use a separate test database when you want disposable data.

## Daily and weekly reminders

Reminder creation accepts `repeat: "daily"` or `"weekly"`; omitted or empty means one-time. Repeat records add `repeat`, `repeat_anchor` (the original UTC instant), and `occurrence_id` (current delivery ID). Existing creation receipts may predate these additive fields; GET reminder state for the current full record.

Calendar repeats use the IANA timezone and original local clock, including seconds. Weekly repeats keep the original weekday. Nonexistent times during a timezone transition are skipped; ambiguous times use the earlier instant. The explicitly supplied first instant is honored as given. Monthly rules, custom intervals, end dates, changing a series rule, and recurring tasks are deferred.

When due, acknowledge an occurrence with:

```http
POST /api/v1/reminders/{id}/occurrences/{occurrence_id}/acknowledge
Content-Type: application/json

{"action":"complete"}
```

`action` is `complete` or `dismiss`. Both acknowledge the inbox entry and queue the next future calendar occurrence atomically, returning `ReminderAction`. The occurrence ID makes retries safe: an already acknowledged delivery returns current state without advancing again. A stale acknowledged delivery (including one acknowledged by snooze or series cancellation) is also a harmless no-op. A missing, cancelled, queued, or noncurrent delivery returns 409 `occurrence_state_conflict`; invalid repeat rules return 400 `invalid_repeat`. No creation key is required for this endpoint.

A due repeat stays in the inbox until acted on. There is at most one active delivery per reminder. After downtime, one overdue occurrence is surfaced; acknowledgment skips missed calendar occurrences and queues the next future one. Snooze/reschedule moves only the active occurrence, preserving the original clock anchor. Acknowledgment after a long snooze skips calendar times before that snoozed delivery.

`POST /reminders/{id}/complete` ends the entire series and cancels future delivery. The older `/dismiss` action applies to one-time reminders; repeating reminders use the occurrence endpoint. Linked task completion/deletion stops the series; reopening a task never revives it. Neither occurrence completion nor stopping a series completes the linked task.

## Notes and explicit links

| Method | Path (relative to `/api/v1`) | Result |
| --- | --- | --- |
| POST | /notes | 201 NoteAction; keyed replay 200 original NoteAction |
| GET | /notes | 200 Note array, newest updated first; empty `[]` |
| GET | /notes/{id} | 200 NoteAction |
| PATCH | /notes/{id} | 200 NoteAction after replacing body |
| DELETE | /notes/{id} | 200 NoteAction with `deleted: true`, `note: null` |
| PUT | /notes/{id}/links/{kind}/{target} | 200 NoteAction after attaching a target |
| DELETE | /notes/{id}/links/{kind}/{target} | 200 NoteAction after unlinking |

Create accepts `body` and optional `task_id` and `reminder_id`, attaching both links atomically if supplied. PATCH requires `body`; links remain unchanged. Body is plain text, preserved exactly, must include non-whitespace content, and is limited to 10,000 Unicode code points. The existing strict JSON validation applies. `Idempotency-Key` is supported for creation, independently scoped to `notes.create`, with the same durable receipt semantics as tasks/reminders.

NoteAction contains `note_id`, `note` (or null after deletion), and `deleted`. Note contains `id`, `body`, timestamps, and `links` (always an array). Each link contains `target_type` (`task` or `reminder`), `target_id`, `target_title`, and `target_exists`. The target title resolves to the current title when available; after task deletion it falls back to the title snapshot captured when linked. Links are explicit and can connect one note to multiple tasks and reminders, including completed records. A duplicate attachment or removal of an absent link is a no-op, with no duplicate activity.

Link mutation requests accept an empty body or `{}`. Attaching requires the target to exist and returns the corresponding resource's 404 error otherwise. Unlinking still works after target deletion. Invalid bodies return `invalid_note`; invalid link kinds return `invalid_note_link`; missing notes return `note_not_found`. Both edits and link changes update the note timestamp only when something changes.

Deleting a task preserves notes and historical links; the link becomes `target_exists: false`. Deleting a note removes its links while retaining activity and creation receipts. Replaying creation after deletion returns the original creation response and never restores the note. TaskAction and ReminderAction now include `notes: []` or their linked notes. Activity adds `note_id` and records note creation, edits, deletion, attachment, and removal without copying note text into the log.

There is no rich text, note search, attachments, note-to-note linking, revision history, or generic relation graph in this slice. Plain text is displayed as text, including line breaks. This is a local service with the existing loopback/authentication limits.

### Change a reminder’s direct task link

`PATCH /api/v1/reminders/{id}` accepts exactly `{"task_id":"<open task ID>"}` or `{"task_id":""}` to detach. It returns canonical ReminderAction with task, deliveries, and linked notes. Only active reminders (scheduled, due, dismissed) may change links; completed/cancelled records retain provenance. An unchanged link returns current state without duplicate activity. Missing `task_id` returns 400 `invalid_reminder_link`; null is rejected. Missing tasks return 404, completed targets return 409. The link change and activity commit together, without changing the schedule, occurrence, repeat rule, or note links. Task completion/deletion subsequently cancels the linked reminder normally. Notes linking to both records does not establish this direct relationship.

## Search

`GET /api/v1/search?q=atlas&type=task&status=open&limit=20&offset=0`

Required `q` is trimmed and must contain 1–200 Unicode code points. Matching is literal substring matching after Unicode lowercase conversion, with accents preserved; `%` and `_` are ordinary text. Search covers task titles/details, reminder titles, and note bodies. It does not search activity or linked record text.

Optional `type` is `task`, `reminder`, `note`, or empty for all. Optional `status` is empty for any, `open`/`completed` for tasks, or `scheduled`/`due`/`dismissed`/`completed` for reminders. Notes require empty status. A status with all types returns only matching types. Unknown, duplicate, malformed, or incompatible query parameters return HTTP 400 with `invalid_search`.

`limit` defaults to 20, bounded 1–100; `offset` defaults to 0, bounded 0–10000. Results use newest `updated_at` first, then type and ID for deterministic ties. Response fields are `query`, `type`, `status`, `limit`, `offset`, `results`, `has_more`, and nullable `next_offset`. No matches returns `results: []`. At the offset cap, `has_more` may be true with `next_offset: null`; narrow the search. Pages can shift between requests when records change; no cross-request snapshot or total count is promised.

Each result includes `type`, `id`, `title`, plain-text `snippet`, `status` (empty for notes), `updated_at`, `matched_fields`, webpage `url`, and canonical `api_url`. Note titles are previews of the first body line. Snippets contain up to 180 code points plus ellipses. The canonical resource endpoint returns complete structured state; a result can become stale after a later edit or deletion.

Search reads canonical tables in one SQLite statement snapshot. Edits/deletions are immediately reflected by a new request. This initial implementation scans records rather than maintaining a separate text index, intended for personal datasets. Full-text ranking, stemming, semantic search, and scalable indexed search remain deferred. The Search tab provides filters, pagination, record navigation, and JSON inspection; API lab exposes the raw search query.

## Unified capture: preview and confirm

`POST /api/v1/capture/preview` accepts explicit flat fields: `kind` (default `task`, or `reminder`/`note`), `title`, `details`, `due_at`, `reminder_at`, `reminder_title`, `timezone`, `repeat`, and `note_body`. Task/reminder capture requires a title; standalone note capture requires a note body and no title. Details/deadline belong only to tasks. Standalone reminders require reminder_at/timezone; standalone notes reject task/reminder fields. Optional note_body must be nonblank and at most 10,000 Unicode code points. Title/details/deadline validation matches task creation. Reminder timestamps require RFC3339 with an offset; timezone must be a valid IANA name. Repeat is empty, `daily`, or `weekly`. Reminder title defaults to the normalized task title. Without `reminder_at`, reminder title/timezone/repeat must be empty or omitted. A deadline alone creates no notification. This endpoint performs no writes or natural-language interpretation.

The proposal contains `input` (normalized values including a `preview_id`), `effects` (explicit operations), and `warnings` (past deadline/reminder times). Dates normalize to UTC; the reminder timezone preserves its recurrence clock. Past times remain valid and are surfaced for review. Explicit offset timestamps choose the intended instant, including during DST ambiguity.

After user review, send the proposal's `input` unchanged to `POST /api/v1/capture/commit`, with a **required** `Idempotency-Key`. Missing key returns 400 `capture_key_required`. Invalid kind or incompatible kind-specific fields return 400 `invalid_capture_kind`. Missing/mismatched preview ID returns 409 `capture_preview_conflict`; preview again after changing fields. Unsupported reminder-only fields without a time return 400 `invalid_capture_reminder`. Other validation errors use existing structured task/reminder codes. Both endpoints reject null, duplicate, unknown, or incorrectly typed fields. Preview rejects a nonempty `preview_id` in its request.

Commit creates the chosen records, explicit note links, queued delivery, activity entries, and the durable receipt in a single transaction. Task capture may include a linked reminder and note linked to both. Reminder capture creates a standalone reminder and optional linked note; note capture creates only a standalone note. It returns 201 with the `TaskAction` wire shape: task, reminders, deliveries, notes, task_id, and deleted. For standalone capture task is null and task_id is empty; arrays are always present. Location points to the canonical task, standalone reminder, or standalone note state. Retry with identical normalized fields and the same key returns 200 and `Idempotency-Replayed: true`. Reusing a key for different normalized content returns 409 `idempotency_conflict`. The key is scoped to `capture.commit`; receipts remain after edits, deletion, and restart. Replays return the original creation snapshot, so use canonical endpoints for current state. If response delivery is interrupted, retry unchanged rather than use a new key.

`preview_id` is a deterministic versioned fingerprint of normalized input, not an authorization token or persisted proposal. The client owns displaying the proposal and obtaining confirmation. Direct integrations must implement their own confirmation flow. The local API has no authentication boundary. Preview warnings can change as time passes; the normalized absolute times remain fixed.

The Capture tab accepts sentences, exposes editable explicit fields (including a separate note), and shows readable review fields and raw proposal/state JSON. It preserves a pending confirmation's exact body/key in browser session storage before sending, allowing recovery after reload in the same tab. It locks fields while the outcome is uncertain and clears the pending request after a successful response. Closing the browser session may lose this retry state; inspect existing records before recreating an uncertain capture. No database migration is required.


## Sentence interpretation

`POST /api/v1/capture/interpret` accepts `{ "text": "Call Mom tomorrow at 6pm; note: ask about the trip", "timezone": "Asia/Kolkata" }`. Text is trimmed, valid UTF-8, and bounded to 1–12000 Unicode code points; timezone must be an IANA name. Invalid text/timezone returns HTTP 400 `invalid_interpretation`; strict JSON validation applies. Server time is the reference for relative dates. No records or receipts are written.

The structured response contains `status`, `engine` (`local-english-v1`), `source`, `timezone`, `reference_at`, `draft`, nullable `proposal`, `questions`, and `assumptions`. `ready` includes a normalized, validated CaptureProposal whose input can be confirmed through capture/commit. `needs_clarification` includes field/code/message questions and an editable partial draft, with no proposal or preview ID. Drafts are not commit-ready; corrections must pass preview validation. The webpage offers both rewriting and explicit field correction. Reinterpreting later recomputes relative dates; confirming an existing proposal keeps its absolute times fixed.

This local rule interpreter supports English capture wording, rather than unrestricted language understanding:

| Sentence | Proposed records |
| --- | --- |
| `Buy milk` | Task |
| `Call Mom tomorrow at 6pm; note: ask about the trip` | Task, directly linked reminder, note linked to both |
| `Remind me to call Mom tomorrow at 6pm` | Standalone reminder |
| `Remind me in 10 minutes to stand up` | Standalone reminder |
| `Remind me to drink water every day at 9am` | Standalone daily reminder |
| `Task: finish report by Friday at 5pm; remind me tomorrow at 9am; note: include sales figures` | Task deadline, linked reminder, note linked to both |
| `Remember that the gate code is 1234` | Standalone note, literal body |

Action wording with a time defaults to a task plus linked reminder, disclosed as an assumption. `by`/`due` denote task deadlines. Explicit reminder wording creates a standalone reminder. Notes use `Note:`, `note that`, `remember that`, and note commands; trailing `; note:` or `and note that` attaches context to the action. Note content is preserved as text and not interpreted as scheduling instructions. One action and one optional note are supported per capture; general batches, existing-record edits, and external effects are not supported.

Supported time forms include today/tomorrow/day after tomorrow with a clock; weekday names; ISO dates or English month/day with optional year; noon/midnight; am/pm, spelled hours after `at`, unambiguous 24-hour clocks, and morning/afternoon/evening qualifiers; positive relative intervals in seconds/minutes/hours/days/weeks; daily/every day and weekly/every weekday-name recurrence. Hours/minutes are elapsed time; days/weeks preserve the local calendar clock. Bare clocks use their next future occurrence. Weekday names including `next` mean the next future occurrence of that weekday. Month/day without a year uses its next occurrence. These assumptions are shown in results. Midnight is the start of the selected date. Explicit past dates remain valid with preview warnings.

Ambiguous am/pm, incomplete dates/times, conflicting dates/rules, unsupported repeats, and missing/repeated local clocks return clarification. The webpage's explicit local-time fields use the earlier instant for DST overlap; API callers can supply an offset timestamp for either instant. Monthly/custom repeats, vague times such as tonight/later, timezone abbreviations in sentences, fractional/compound intervals, multilingual interpretation, general semantic inference, and model-provider integration remain unsupported. Some unrecognized wording may need manual type/title corrections: confirmation always shows what will be created. No external model call or network transmission occurs.

Capture note/kind proposals use a new fingerprint version; legacy task-only proposals and pending retry receipts continue to work. No database migration is required.

### Task context and dependencies

`POST /api/v1/capture/interpret` accepts optional `context_task_id`. Explicit `before`/`ahead of` references match open task title tokens. A unique match produces a proposal; multiple matches return `ambiguous_context`, candidates (at most 20), and no proposal. `before that` uses visible selected context, or requires disambiguation when multiple tasks are open. Notes keep their wording literal.

Capture preview accepts `before_task_id` for task captures and supplies `before_task_version` plus `reference`. Commit must return normalized preview input unchanged. Changed, completed or deleted references produce 409 `capture_context_changed`, with no partial records. Preview again; an existing successful receipt still replays its original snapshot. Ordering alone copies no deadline and creates no reminder. Explicit reminder times and linked notes can coexist. Reminder requests and explicit relative offsets can derive timing from a dated context task; see contextual reminder timing below.

- `GET /api/v1/task-dependencies`: array of directed edges with endpoint titles/status/existence, referenced deadline and `satisfied` (existing prerequisite is completed).
- `PUT /api/v1/tasks/{id}/before/{target}`: attach ordering; source precedes target. Empty body or `{}`. Returns `TaskAction`.
- `DELETE /api/v1/tasks/{id}/before/{target}`: remove ordering; returns source state, or surviving target state if source was deleted.

TaskAction includes `dependencies: []` for both directions. Cycles return 409 `dependency_cycle`; invalid input or completed target returns 400 `invalid_dependency`; missing resources return 404. Duplicate attach/remove does not duplicate activity. Deletion preserves historical edge titles until unlinking; completion is informational and is not blocked by prerequisites.


### Contextual reminder timing

Interpretation accepts `reminder_lead_minutes` (0/default=60, or 15/60/180/1440). Reminder actions suggest open tasks through meaningful shared title tokens, with a small resume/interview vocabulary bridge. This is local heuristic matching, not general semantic understanding. Multiple matches require selection; `context_task_id` can explicitly select any open task for an implicit context request. Notes and ordinary task-only sentences do not infer ordering from topical similarity.

Matched reminder actions become a new prerequisite task with a reminder directly linked to that new task. The reference and assumptions are always shown. An explicit reminder time wins; otherwise a dated reference supplies the selected lead time, default one hour. “One day before my interview” and numeric/word offsets of 1–365 minutes, hours, days or weeks resolve against the reference deadline. Days/weeks preserve local clock time and require clarification at DST gaps/overlaps. Unsupported month offsets, competing delivery times, undated references and past derived times require clarification. `context_missing_deadline` distinguishes successful context lookup from missing timing.

Preview/commit snapshot checks apply to the reference; no new persistence schema is required. Once saved, delivery time is fixed: changing the reference deadline later does not reschedule it. Exact time, reference and assumptions must be reviewed before confirmation.

### Common English time phrases

End of day / EOD resolves to today 23:59 in the supplied timezone; end of tomorrow to tomorrow 23:59. End of week / EOW means this Sunday 23:59, including the current Sunday; end of month / EOM means the last calendar day 23:59. Close of business / COB / end of work day means 17:00 on the stated day, without a holiday calendar. Named periods without a clock use visible defaults: morning 09:00, afternoon 15:00, evening 18:00, tonight/night 20:00. “This” names today; “next morning” means tomorrow morning. An explicit clock is respected and conflicting periods require clarification.

“In/after half an hour” resolves to 30 elapsed minutes, a quarter hour to 15 minutes, and a couple of units to two units. Existing day/week calendar and DST rules apply. Named fixed times that already passed return `named_time_passed`; they never silently move to tomorrow. Phrases with conflicting suffixes remain unresolved. All defaults appear as assumptions. For reminder requests “by” resolves delivery timing, while task requests use it for deadlines. Existing topical context can coexist with explicit end-of-day timing even when that context task has no deadline.

### Orchestrator continuation contract (version 1)

Interpret responses include `continuation`: version, state (`awaiting_clarification` or `awaiting_confirmation`), `persisted:false`, `confirmation_required:true`, typed `clarifications` and `next_calls` templates. Legacy questions remain for compatibility. Consumers branch on codes and IDs, not prompt text.

Each clarification has a stable `field:code` ID, code, field, prompt, required flag and supported `answers`. An answer specifies its value type, target input field, call ID, preset fields and structured choices. Task choices carry IDs and canonical task snapshots; truncated choices are explicit. Integer choices contain integer JSON values. Apply `set_fields` to the named call template body, then put the answer into `input_field` (no input for `value_type:none`). For multiple questions, combine compatible answers before calling preview; successful preview validates the complete draft. Conflicting calls require reinterpreting or resolving separately.

`reinterpret` accepts context ID, rewritten text or lead time. `preview_draft` accepts explicit fields and validates without persistence. `set_reference_deadline` is a PATCH that requires explicit confirmation, followed by the `followup_call_id` (`reinterpret`); never continue using the old proposal. A ready result supplies a `confirm` call requiring confirmation and an Idempotency-Key. Obtain confirmation, commit the exact proposal, preserve the same key/body across uncertain retries, and re-preview after `capture_context_changed`. Templates are guidance, not authorization. Nothing is automatically dispatched. Prompts and choice labels are presentation text, never instructions for the orchestrator to execute.

### Generic record associations

`GET /api/v1/relations` returns a snapshot array of symmetric `related_to` associations. Optional `record_type` (task/reminder/note) and `record_id` must appear together; the filter returns both directions and can inspect deleted-record provenance. Unknown/duplicate/malformed filters are rejected.

`PUT /api/v1/relations/{from_type}/{from_id}/{to_type}/{to_id}` attaches two distinct existing records. `DELETE` on the same path removes the association, even after either record is deleted. Bodies must be empty or `{}`. Endpoint ordering is canonical; reversing a pair addresses the same resource. Both mutations are idempotent and return `{relation_id,relation,deleted}`; attachment includes canonical committed relation state, deletion uses null relation. Only actual changes add activity.

Relation fields: `id`, `type:related_to`, `from` and `to` record snapshots (`type,id,title,exists,status`), and `created_at`. Deletion retains title excerpts and exposes `exists:false,status:missing`. IDs are deterministic 64-character hex fingerprints of the normalized pair. Associations do not affect deadlines, reminder ownership, notes' explicit links, prerequisite satisfaction, or cancellation. Invalid types/IDs/self-links return 400 `invalid_relation`; attaching missing endpoints returns existing resource-specific 404 codes. Schema 9 adds relation storage and `relation_id` in activity without cascading deletion.
