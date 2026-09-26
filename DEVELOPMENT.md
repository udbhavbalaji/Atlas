# Development workflow

`main` tracks major verified milestones. `development` integrates ongoing work.

1. Start each feature from `development`: `git switch development`, then `git switch -c codex/<feature>`.
2. Implement and verify the feature. Run `make check` and update relevant documentation.
3. Commit on the feature branch and merge with `git merge --no-ff codex/<feature>` on `development`.
4. At a major verified milestone, merge `development` into `main` with `--no-ff`.

## Milestones

- [x] Foundation: local Go service, SQLite migration, task capture/list/completion, atomic activity history.
- [ ] Task editing/deletion, notes, and explicit record relations.
- [ ] Fixed-time reminders with durable delivery, restart recovery, snooze, and completion.
- [ ] iPhone text client proving the capture-to-reminder loop.
- [ ] Validated request orchestration and opt-in Jev interpretation.

The first foundation is an integration milestone on `development`. The first complete text reminder loop is the intended first major milestone for `main`.

## Foundation API

All endpoints use `/api/v1`, except `GET /healthz`.

| Method | Path | Result |
| --- | --- | --- |
| POST | /api/v1/tasks | Create from `{ "title": "Call Mom" }`; returns 201 and task |
| GET | /api/v1/tasks | Tasks, newest first; empty result is `[]` |
| POST | /api/v1/tasks/{id}/complete | Complete; returns 204, including repeated completion |
| GET | /api/v1/activity | Latest 100 entries, newest first |

IDs are random 128-bit hex strings. Timestamps are UTC RFC3339. Task titles are trimmed and limited to 500 Unicode code points. Invalid input returns 400, missing tasks 404, storage failures 500. JSON errors have an `error` field. Task writes and activity entries share one transaction. SQLite `user_version` tracks migrations; a newer database schema is rejected.

Creation currently creates a new task on every call. Client request IDs and idempotent capture will be added with request orchestration. There is no reminder delivery, natural-language interpretation, authentication, or PWA yet. The service binds to loopback by default; keep it local during this stage.
