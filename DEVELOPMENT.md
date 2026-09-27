# Development workflow

`main` tracks major verified milestones. `development` integrates ongoing work.

1. Start each feature from `development`: `git switch development`, then `git switch -c codex/<feature>`.
2. Implement and verify the feature. Run `make check` and update relevant documentation.
3. Commit on the feature branch and merge with `git merge --no-ff codex/<feature>` on `development`.
4. At a major verified milestone, make the case for promoting `development` into `main`. Wait for explicit user approval before merging with `--no-ff`. Never promote autonomously.

## Milestones

- [x] Foundation: local Go service, SQLite migration, task capture/list/completion, atomic activity history.
- [x] Usable task subsystem: responsive browser interface, editing/deletion, completion/reopening.
- [x] Task details and optional exact deadlines, editing/clearing, overdue view.
- [x] Daily/weekly recurring reminders, occurrence acknowledgment, and repeat testing controls.
- [x] Persistent notes with explicit task/reminder links.
- [x] Text search across tasks, reminders, and notes.
- [x] Generic symmetric record associations with structured state and a Relations testing tab.
- [x] Explicit unified capture with preview and atomic task/reminder confirmation.
- [x] Fixed-time reminders with durable webpage inbox delivery, restart recovery, snooze, dismissal, and completion.
- [x] Task-linked reminder creation, inspection, snooze, and transactional cancellation.
- [ ] iPhone text client proving the capture-to-reminder loop.
- [x] Validated capture orchestration and local English sentence interpretation.
- [x] Persistent capture conversations, typed replies, resume, and confirmation recovery.
- [ ] Jev adapter for semantic decisions, bounded context requests, and validated proposals.
- [ ] Optional reminder/note enrichment before task confirmation through Jev.

The first main milestone covers tasks and fixed-time reminders. The second verified milestone adds task links, structured API contracts, and daily/weekly reminder recurrence, promoted through development into main. The third verified milestone adds linked notes, focused testing tabs, text search, and explicit atomic capture, promoted through development into main. The fourth verified milestone adds atomic note/standalone capture and local English sentence interpretation, promoted through development into main. External reasoning providers and external notifications remain future milestones.

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

Creation supports durable optional Idempotency-Key receipts; see [API.md](API.md) for the revision 2 contract and retry semantics. The responsive task interface is served at `/`. Durable webpage reminder delivery is implemented. Local English sentence interpretation is available in Capture; external reasoning providers, authentication, PWA installation, and an offline queue are deferred. The service binds to loopback by default; keep it local during this stage.

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

The webpage polls every two seconds. The server must run for delivery; it catches up after downtime, and delivered inbox entries survive both browser closure and server restart. Push and OS notifications are deferred; local sentence interpretation is available in Capture. Daily/weekly recurrence is implemented as described below. Explicit timestamp offsets choose the intended instant during DST changes; the browser rejects nonexistent local times and uses the earlier occurrence for an ambiguous fall-back time. The API accepts explicit offsets for either occurrence.

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

Schema version 7 adds notes, explicit note links, and note references in activity. Existing tasks, repeats, deliveries, and request receipts are retained. Generic relations remain future work; text search is now available.

### Direct reminder link controls

The general reminder form includes **Link reminder to task (optional)**. Existing active reminders have **Link to task** / **Change task link**: choose an open task and save, or select **Standalone reminder** to detach. Verify the linked task appears on the reminder and the task shows it under Linked reminders. Complete the task and verify cancellation. No note is required; note links only associate context.

## Feature testing interface

Use the Capture, Tasks, Reminders, Notes, Search, Activity, and API lab tabs to work on one feature at a time. Each tab has a What you can test checklist. The selected tab is stored in the URL fragment and survives browser reload; unsaved form contents remain while switching tabs. A due count on Reminders stays visible from other tabs. Add note / View note / View task shortcuts open the appropriate tab. Use arrow keys, Home, and End on the feature tabs. The dark interface adapts to narrow screens. Refresh data reloads saved records; browser reload loads interface updates.

## Test search

1. Save distinctive text in a task's details, a reminder title, and a note body. Open **Search** and search for that text; all matching record types should appear.
2. Change **Record type** and **Status**; notes offer Any status only. Try different letter case and literal `%` or `_` text.
3. Click **Open task/reminder/note** to reveal and focus the matching record. Completed tasks remain visible; completed/dismissed reminders open history. Reload a record URL to verify navigation persists.
4. Edit a matching record and search again. New results should reflect the edit. Inspect **JSON state** for full canonical state.
5. With more than 20 matches, use Next/Previous page. API lab supports GET search with a raw query such as `q=atlas&limit=1`; inspect pagination and `matched_fields`.
6. In API lab try missing `q`, duplicate `q`, or `type=note&status=completed`. Expect structured HTTP 400 `invalid_search`.

Search introduces no database migration. It scans canonical records; index-backed search and semantic retrieval remain deferred.

## Test unified capture

1. Open **Capture**, enter a title/details and optional deadline, then enable **Add a linked reminder** and choose a time and repeat.
2. Click **Preview capture**. Review the displayed times, timezone, repeat, and structured proposal. Switch to Tasks/Reminders to verify no records were created.
3. Edit a field: the preview disappears and must be regenerated. Confirm the regenerated preview to save the task and reminder together.
4. Open the saved task/reminder links. Verify the direct link, occurrence ID, and queued delivery in structured saved state. Complete the task and confirm its reminder is cancelled.
5. Test a past deadline/reminder time: preview warns; confirmation preserves the explicit time and the worker delivers the reminder.
6. In API lab select Preview capture, then copy response `input` into Confirm capture. Use a retry key; replay creates no duplicates. Change fields without regenerating preview to see `capture_preview_conflict`; regenerate and reuse the same committed key to see `idempotency_conflict`.
7. In an isolated test server, stop Atlas after preview and click Confirm. Fields lock and Retry same confirmation appears. Restart Atlas, reload the same tab, and retry; one task/reminder is saved. Session storage preserves the body and key within that browser session.

Atomic rollback, concurrent retry, receipt replay after restart/deletion, task-only capture, validation, and linked cancellation are covered by automated tests. Capture introduces no schema migration. Its explicit preview endpoint does not interpret text; sentence interpretation is available separately.


## Test sentence capture and notes

1. In Capture enter `Call Mom tomorrow at 6pm; note: ask about the trip`, then Interpret sentence. Check the local date/time, task title, linked reminder, note body, and note link targets. Nothing is saved yet.
2. Confirm and open each saved record. The note links to both the task and reminder. Reload and use Search to verify persistence.
3. Start a new capture with `Remind me to drink water every day at 9am`. Confirm and verify a standalone recurring reminder with no task.
4. Enter `Remember that the gate code is 1234`. Confirm and verify just a standalone note, with no accidental task or reminder.
5. Enter `Call Mom tomorrow at 6`. There must be an AM/PM clarification and no confirmation button. Rewrite to `6pm`, or set explicit fields and Preview capture.
6. Try `Task: finish report by Friday at 5pm; remind me tomorrow at 9am; note: include sales figures`. The task deadline and reminder are separate times. Edit the note before previewing again and confirm.
7. Try missing time, invalid dates, monthly recurrence, and unclear record type. Inspect field/code/message questions via API lab. Correct the fields before preview/confirmation.
8. Existing confirmation retry behavior remains: an interrupted confirmation preserves its exact request through same-tab reload. Note content participates in the fingerprint and receipt; changing it requires a new preview.

The automated corpus covers supported phrasing, alternate word orders, Unicode content, AM/PM ambiguity, invalid dates, DST gaps/overlaps, calendar-day versus elapsed-hour intervals, and API interpretation-to-commit. Storage tests inject note-link failures and verify rollback of every record/link/activity/receipt. Browser tests use a separate database. This is a bounded local English interpreter, not unrestricted language understanding.

## Latest capture and relation milestones

Context selection, reviewed relative reminder timing, common time idioms and typed orchestrator continuations are implemented. Schema version 9 adds generic record associations and relation activity IDs, preserving notes, dependencies and capture receipts. The next client milestone remains an iPhone text client; external interpretation and external notifications remain separate future work.

## Test capture conversations

1. In Tasks create two dated interviews with different company names. In Capture's **Conversation test**, start `Remind me to print my resume before that`. Check context clarification and no new action records.
2. Reply `the Ather one`. Review the referenced interview and reminder one hour before it. Reply `one day before` to adjust; the original request and context stay intact.
3. Reload the browser or restart Atlas. The last session resumes automatically; its ID also works in **Resume session**.
4. Reply `yes`; inspect saved JSON for task, reminder, and dependency. Repeat `yes` through the API; the original receipt returns and no duplicates appear.
5. Start `Remind me to call Mom`, then reply `tomorrow at 9am`. Confirm, or cancel and verify no new records. Try a note sentence or an action with `; note:`.
6. An unknown reply must keep the question open. Two interviews matching the same reply must not auto-select. Use the visible context choice buttons or a more specific title.
7. Change the reference task's deadline before confirming. Confirmation refreshes the proposal without saving; review the new timing and confirm again.
8. API clients can GET by ID and POST replies using `next_request.body.version`. A stale version returns a structured 409. Reply `replace: Call Mom tomorrow at 6pm` to intentionally replace the original request.

Schema 10 adds capture session documents without changing existing action records. The interpreter remains rule-based. Unrestricted conversational language, external notifications, and the iPhone client remain future work.

### Interview capture regression

Start `I have an interview at Ather on Tuesday`; expect a task proposal waiting for a clock time. Reply `3pm`, verify both the task deadline and linked reminder are Tuesday at 3 PM, then confirm. Existing sessions created before this fix can reply `action` to recover. Generic unclear sentences also accept `action` as a synonym for `task`; the test UI offers Action / task, Reminder, and Note buttons for record-type clarification. Jev now owns the target architecture for semantic decisions; ongoing phrase-rule expansion is deferred in favor of its adapter.


## Current readiness and next milestone

Tasks, reminders, durable webpage inbox delivery, notes, links/relations, search, structured state APIs, and persistent capture sessions are implemented and testable locally. Automated validation includes race checks and confirmation/restart recovery; the testing webpage is the current client. This is a local core milestone, not the complete iPhone product.

Next: verify Jev's actual integration contract, implement the bounded adapter and inspectable proposal/context payloads, then implement optional reminder/note enrichment in the conversation UI with task-only skip. Keep Atlas validation, explicit confirmation, and atomic persistence authoritative. Prove the flow on existing test UI before the iPhone PWA and real notification delivery work. Webpage inbox delivery requires the Atlas server to run; external push notifications, authentication, sync, PWA installation, and offline queues remain unimplemented.

## Test provider foundation

Open **Providers**. The screen must label everything as mock and mark Jev unavailable.

1. Run the Task fixture with both additions set to Ask me. Inspect structured optional questions; verify no records appear in Tasks/Notes/Reminders.
2. Choose Add reminder and Attach note, supply a time and note, and run another preview. Confirm once; inspect saved state for the direct reminder link and note links. Change a field before confirming: the prior proposal must disappear.
3. Use **Save task only — preview first**. Review, then confirm; no reminder/note is created.
4. Create a distinctive task, select the context fixture and query its title. Inspect both exchanges and the minimized context. Choose its returned ID; review the before relationship, then confirm. Over five matches must report truncation.
5. Try invalid output and unavailable fixtures; inspect rejected/unavailable traces and verify no records are created.
6. In an isolated server, interrupt confirmation, restart, reload the same tab, and retry the retained proposal/key. Existing receipt recovery returns one saved result.

Provider foundation introduces no migration. Contract tests cover schema/status violations, unexpected/unseen references, context limits and repeated requests, malformed reminder fields, atomic additions, idempotent capture, and unavailable-provider behavior. It does not replace live Capture conversations yet; wire Jev and durable provider sessions after verifying its actual interface.

## Routing milestone and Jev setup

The routing layer is implemented separately from the interim Capture interpreter. Open **Routing** to inspect the registry, probabilities and channel handoff. Channel field extraction currently requires explicit review/input.

For Gateway access, save only the key (no quotes or assignment) in `/home/udawg_00/Developer/atlas/data/ai-gateway.key`. This directory is ignored by Git. Restrict the file to mode 600. Atlas reads it at startup; `AI_GATEWAY_API_KEY` takes precedence. Override its path with `-gateway-key-file`, or use an empty flag to disable file loading. No npm setup command is required. Vercel returned HTTP 403 requiring a credit card for this account; included credits did not bypass verification. User declined a card, so further live evaluations are paused. Do not silently substitute another model.

Manual tests:

1. Select Mock, Task, and enter the interview sentence. Evaluate: inspect five action probabilities and task channel questions. No action records are created.
2. Supply the reviewed title and deadline. Choose Skip reminder / Skip note and Dispatch / preview. Confirm; verify one task only.
3. Repeat with Add reminder / Add note, supply timing and contents, preview and confirm. Inspect canonical task/reminder/note links.
4. Use the Reminder or Note fixture and exercise their required fields. They must not create an accidental task.
5. Use Ambiguous: evaluation must wait for your explicit channel selection and dispatch click. Unsupported/malformed/unavailable cases must offer no confirmation.
6. Change source/provider/context: evaluation is invalidated. Change channel fields: preview is invalidated without another evaluation. A restored uncertain confirmation retries the same storage key through reload/restart.
7. Optional context search is bounded to five open tasks; only supplied IDs can be selected as prerequisite targets. Changing canonical state still requires reviewing the refreshed preview.
8. When actual Gateway access becomes available, explicitly choose Jev and evaluate a synthetic sentence once. A repeated unchanged evaluation should return `cache_hit:true`, `model_calls:0`. Inspect the originating token usage and cost. Changing relevant context must miss the cache.

Automated routing tests simulate HTTP/authentication, malformed output, cancellation, redirects, uncertainty, bounded caching/concurrent reuse, channel questions, token tampering, cross-server receipts and atomic idempotent capture. They make no actual Jev calls. The next natural-language milestone is channel-owned extraction/decision handling and durable conversations using the routing boundary.

## Branch promotion policy

Use feature branches from `development`; tested work may merge into `development`. Never merge into `main` without explicit user approval. At a substantial milestone, make the case for promotion with scope, verification and limitations, then await approval. This supersedes earlier autonomous main promotion guidance.

### Routing source and context prefill test

1. In Routing, use Mock / Task and `I have an interview at Ather on Tuesday at 3pm; note: bring portfolio`. Evaluate: the secondary form shows the source, fills title/deadline/reminder/note, and explains local-parser suggestions. It must not offer confirmation until Dispatch / preview.
2. Use `I have an interview at Ather on Tuesday` instead: title fills, timing remains blank with a missing-clock warning. No arbitrary date/time is invented.
3. Enter a context search for an existing dated interview. The channel form displays supplied records and explicit Copy context title/deadline and prerequisite buttons. Copy values, edit them, preview; source and context remain in the structured channel response. Searching by itself must not copy or link a record.
4. Edit or clear a seeded field after preparation. Normal Dispatch / preview must preserve that change. Skip reminder/note must remove those effects. Unchanged suggested/copied date controls must retain their original instant.
5. Reminder fixtures can seed reminder title/time/repeat; Note fixtures can seed parsed note contents or retain the sentence verbatim. Mock routing remains fixture-driven; prefill is a separate local helper and makes no Jev calls.
