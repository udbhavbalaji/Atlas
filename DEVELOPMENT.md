# Development workflow

`main` tracks major verified milestones. `development` integrates ongoing work.

1. Start each feature from `development`: `git switch development`, then `git switch -c codex/<feature>`.
2. Implement and verify the feature. Run `make check` and update relevant documentation.
3. Commit on the feature branch and merge with `git merge --no-ff codex/<feature>` on `development`.
4. At a major verified milestone, merge `development` into `main` with `--no-ff`.

## Milestones

- [x] Foundation: local Go service, SQLite migration, task capture/list/completion, atomic activity history.
- [x] Usable task subsystem: responsive browser interface, editing/deletion, completion/reopening.
- [x] Task details and optional exact deadlines, editing/clearing, overdue view.
- [ ] Notes and explicit record relations.
- [x] Fixed-time reminders with durable webpage inbox delivery, restart recovery, snooze, dismissal, and completion.
- [ ] iPhone text client proving the capture-to-reminder loop.
- [ ] Validated request orchestration and opt-in Jev interpretation.

The local task and fixed-time reminder subsystem is the first major milestone promoted from `development` to `main`. Natural-language capture, external notifications, recurrence, and task/reminder links remain future milestones.

## Foundation API

All endpoints use `/api/v1`, except `GET /healthz`.

| Method | Path | Result |
| --- | --- | --- |
| POST | /api/v1/tasks | Create from `{ "title": "Call Mom" }`; returns 201 and task |
| PATCH | /api/v1/tasks/{id} | Update supplied `title` and/or `status` (`open` or `completed`); returns task |
| DELETE | /api/v1/tasks/{id} | Permanently delete; returns 204; retains activity history |
| GET | /api/v1/tasks | Tasks, newest first; empty result is `[]` |
| POST | /api/v1/tasks/{id}/complete | Complete; returns 204, including repeated completion |
| GET | /api/v1/activity | Latest 100 entries, newest first |

IDs are random 128-bit hex strings. Timestamps are UTC RFC3339. Task titles are trimmed and limited to 500 Unicode code points. Invalid input returns 400, missing tasks 404, storage failures 500. JSON errors have an `error` field. Task writes and activity entries share one transaction. SQLite `user_version` tracks migrations; a newer database schema is rejected.

Creation currently creates a new task on every call. Client request IDs and idempotent capture will be added with request orchestration. The responsive task interface is served at `/`. There is no reminder delivery, natural-language interpretation, authentication, PWA installation, or offline queue yet. The service binds to loopback by default; keep it local during this stage.

## Task details and deadline contract

POST accepts optional `details` (up to 10,000 Unicode code points) and `due_at` (an RFC3339 timestamp including timezone). PATCH accepts those same fields. Omitted fields remain unchanged; an empty string clears details or the deadline. Deadlines are normalized to UTC. Invalid dates and timestamps without timezone are rejected. Existing schema version 1 databases migrate transactionally to version 2 without losing tasks or activity. Every feature includes a browser testing interface as recorded in DECISIONS.md.

## Fixed-time reminder contract

| Method | Path | Result |
| --- | --- | --- |
| POST | /api/v1/reminders | Create with `title`, `scheduled_at` (RFC3339 with offset), and `timezone` (IANA); returns 201 and reminder |
| GET | /api/v1/reminders | All reminders ordered by scheduled time |
| POST | /api/v1/reminders/{id}/snooze | Supply a future `scheduled_at`; returns 204 |
| POST | /api/v1/reminders/{id}/dismiss | Acknowledge a due reminder; returns 204 |
| POST | /api/v1/reminders/{id}/complete | Complete and cancel queued delivery; returns 204 |
| GET | /api/v1/deliveries | Latest 100 delivery records |

Reminder states: scheduled → due → dismissed/completed. Snoozing a scheduled, due, or dismissed reminder returns it to scheduled with a new delivery; completed reminders cannot be snoozed. Dismissing acknowledges the inbox notification without marking the reminder completed; a dismissed reminder can still be rescheduled. Repeated completion and dismissal are idempotent. Reminders are standalone in this milestone; task completion and reminder completion are independent.

Schema version 3 adds reminders, deliveries, and reminder references in activity history. Creation atomically writes reminder, queued delivery, and activity. The worker checks at startup and every second, processing up to 100 due deliveries per transaction. Delivery means publication into the durable webpage inbox, with a delivered timestamp and activity entry committed together. There is no external notification side effect. Failed transactions leave deliveries queued and are retried on the next tick. Snooze acknowledges delivered records, cancels queued ones, and creates a new queued delivery in one transaction. Completion acknowledges delivered records and cancels queued ones.

The webpage polls every two seconds. The server must run for delivery; it catches up after downtime, and delivered inbox entries survive both browser closure and server restart. Push notifications, OS notifications, task links, recurrence, and natural-language interpretation are deferred. Explicit timestamp offsets choose the intended instant during DST changes; the browser rejects nonexistent local times and uses the earlier occurrence for an ambiguous fall-back time. The API accepts explicit offsets for either occurrence.
