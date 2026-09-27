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
- [x] Daily/weekly recurring reminders, occurrence acknowledgment, and repeat testing controls.
- [x] Persistent notes with explicit task/reminder links.
- [ ] Generic record relations and note search.
- [x] Fixed-time reminders with durable webpage inbox delivery, restart recovery, snooze, dismissal, and completion.
- [x] Task-linked reminder creation, inspection, snooze, and transactional cancellation.
- [ ] iPhone text client proving the capture-to-reminder loop.
- [ ] Validated request orchestration and opt-in Jev interpretation.

The first main milestone covers tasks and fixed-time reminders. The second verified milestone adds task links, structured API contracts, and daily/weekly reminder recurrence, promoted through development into main. Natural-language capture and external notifications remain future milestones.

## Foundation API

All endpoints use `/api/v1`, except `GET /healthz`.

| Method | Path | Result |
| --- | --- | --- |
| POST | /api/v1/tasks | Create from `{ "title": "Call Mom" }`; returns 201 and task |
| PATCH | /api/v1/tasks/{id} | Update supplied `title` and/or `status` (`open` or `completed`); returns task state |
| DELETE | /api/v1/tasks/{id} | Permanently delete; returns 200 task state; retains activity history |
| GET | /api/v1/tasks | Tasks, newest first; empty result is `[]` |
| POST | /api/v1/tasks/{id}/complete | Complete; returns 200 task state, including repeated completion |
| GET | /api/v1/activity | Latest 100 entries, newest first |

IDs are random 128-bit hex strings. Timestamps are UTC RFC3339. Task titles are trimmed and limited to 500 Unicode code points. Invalid input returns 400, missing tasks 404, storage failures 500. JSON errors have an `error` field. Task writes and activity entries share one transaction. SQLite `user_version` tracks migrations; a newer database schema is rejected.

Creation supports durable optional Idempotency-Key receipts; see [API.md](API.md) for the revision 2 contract and retry semantics. The responsive task interface is served at `/`. Durable webpage reminder delivery is implemented. Natural-language interpretation, authentication, PWA installation, and an offline queue are deferred. The service binds to loopback by default; keep it local during this stage.

## Task details and deadline contract

POST accepts optional `details` (up to 10,000 Unicode code points) and `due_at` (an RFC3339 timestamp including timezone). PATCH accepts those same fields. Omitted fields remain unchanged; an empty string clears details or the deadline. Deadlines are normalized to UTC. Invalid dates and timestamps without timezone are rejected. Existing schema version 1 databases migrate transactionally to version 2 without losing tasks or activity. Every feature includes a browser testing interface as recorded in DECISIONS.md.

## Fixed-time reminder contract

| Method | Path | Result |
| --- | --- | --- |
| POST | /api/v1/reminders | Create with `title`, `scheduled_at` (RFC3339 with offset), and `timezone` (IANA); returns 201 and reminder |
| GET | /api/v1/reminders | All reminders ordered by scheduled time |
| POST | /api/v1/reminders/{id}/snooze | Supply a future `scheduled_at`; returns 200 reminder state |
| POST | /api/v1/reminders/{id}/dismiss | Acknowledge a due reminder; returns 200 reminder state |
| POST | /api/v1/reminders/{id}/complete | Complete and cancel queued delivery; returns 200 reminder state |
| GET | /api/v1/deliveries | Latest 100 delivery records |

Reminder states: scheduled → due → dismissed/completed. Snoozing a scheduled, due, or dismissed reminder returns it to scheduled with a new delivery; completed reminders cannot be snoozed. Dismissing acknowledges the inbox notification without marking the reminder completed; a dismissed reminder can still be rescheduled. Repeated completion and dismissal are idempotent. Reminders can be standalone or explicitly linked to an open task. Task completion/deletion cancels its active linked reminders; reminder completion does not complete its task.

Schema version 3 adds reminders, deliveries, and reminder references in activity history. Creation atomically writes reminder, queued delivery, and activity. The worker checks at startup and every second, processing up to 100 due deliveries per transaction. Delivery means publication into the durable webpage inbox, with a delivered timestamp and activity entry committed together. There is no external notification side effect. Failed transactions leave deliveries queued and are retried on the next tick. Snooze acknowledges delivered records, cancels queued ones, and creates a new queued delivery in one transaction. Completion acknowledges delivered records and cancels queued ones.

The webpage polls every two seconds. The server must run for delivery; it catches up after downtime, and delivered inbox entries survive both browser closure and server restart. Push notifications, OS notifications, and natural-language interpretation are deferred. Daily/weekly recurrence is implemented as described below. Explicit timestamp offsets choose the intended instant during DST changes; the browser rejects nonexistent local times and uses the earlier occurrence for an ambiguous fall-back time. The API accepts explicit offsets for either occurrence.

## Task links

`POST /api/v1/reminders` accepts an optional `task_id`; omitted or empty creates a standalone reminder. A nonexistent task returns 404, a completed task returns 409. GET reminders includes `task_id`, `task_title` (creation snapshot), and `cancellation_reason`. Schema version 4 adds these fields and an index, preserving standalone reminders and deliveries. Link IDs remain historical provenance after task deletion; they are deliberately not cascading foreign keys. Validation and task/reminder writes occur in one transaction.

Both task completion paths (PATCH and POST complete) and deletion cancel all linked reminders whose status is not already completed. They remain `completed` with `cancellation_reason` equal to `task.completed` or `task.deleted`, making the cause explicit. Repeated completion adds no cancellation duplicates. Reopening never queues old occurrences. The scheduler requires linked tasks to exist and be open. Each task supports multiple independently scheduled reminders.

## Structured API contracts

[API.md](API.md) describes structured state responses, stable errors, strict validation, creation idempotency, and the webpage API lab tab. `/openapi.json` serves response and request schemas. Schema version 5 adds durable request receipts without changing existing tasks, reminders, or history.

## Test recurring reminders

1. Enter a reminder title, choose **Daily** or **Weekly**, and click **Test in 5 seconds**.
2. When it reaches the inbox, click **Complete occurrence**. It moves to Upcoming reminders with its next calendar time; inspect the response in the API lab tab.
3. Replay that acknowledgment in the API panel using the original occurrence ID. It must not advance a second time.
4. Snooze the upcoming repeat. Its visible delivery time changes; the repeat clock shown beneath it stays fixed.
5. Click **Stop repeating**. The record moves to history and no new occurrence is queued.
6. Add a weekly repeat from an open task’s **Add reminder** form. Complete the task and verify cancellation. Reopening must not restart it.

Schema version 6 adds repeat rules, immutable clock anchors, and active occurrence IDs. Migration fills active IDs for existing one-time reminders and preserves records, deliveries, activity, and request receipts. Daily/weekly repeat semantics and limits are described in [API.md](API.md).

## Test persistent linked notes

1. Create a task and click **Add note** on its card. Enter multiline context and save.
2. Expand **Linked notes** on the task. The saved note should also appear in the Notes section.
3. Edit the note; its linked display updates too. Refresh the browser or restart Atlas and confirm persistence.
4. On a note card, choose task/reminder and a target, then **Add link**. Repeating the same attachment must not duplicate the link or activity. Use **Unlink task/reminder** to remove it.
5. Inspect **Task state**, **Reminder state**, and **Note state** in the API lab tab. Linked notes and their explicit target metadata are returned in JSON.
6. Create a note via the API panel and click **Replay last request**. Confirm one note and the same ID.
7. Delete a disposable linked task. Its note stays, with the task labelled `(deleted)`. Deleting the note removes its links but retains activity history.

Schema version 7 adds notes, explicit note links, and note references in activity. Existing tasks, repeats, deliveries, and request receipts are retained. Generic relations and note search remain future work.

### Direct reminder link controls

The general reminder form includes **Link reminder to task (optional)**. Existing active reminders have **Link to task** / **Change task link**: choose an open task and save, or select **Standalone reminder** to detach. Verify the linked task appears on the reminder and the task shows it under Linked reminders. Complete the task and verify cancellation. No note is required; note links only associate context.

## Feature testing interface

Use the Tasks, Reminders, Notes, Activity, and API lab tabs to work on one feature at a time. Each tab has a What you can test checklist. The selected tab is stored in the URL fragment and survives browser reload; unsaved form contents remain while switching tabs. A due count on Reminders stays visible from other tabs. Add note / View note / View task shortcuts open the appropriate tab. Use arrow keys, Home, and End on the feature tabs. The dark interface adapts to narrow screens. Refresh data reloads saved records; browser reload loads interface updates.
