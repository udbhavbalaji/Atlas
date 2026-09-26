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
- [ ] Fixed-time reminders with durable delivery, restart recovery, snooze, and completion.
- [ ] iPhone text client proving the capture-to-reminder loop.
- [ ] Validated request orchestration and opt-in Jev interpretation.

The first foundation is an integration milestone on `development`. The first complete text reminder loop is the intended first major milestone for `main`.

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
