# Atlas record workflow audit

This inventory covers the record operations exposed by the current store, HTTP API, and routing conversation. “Conversation” means `/api/v1/conversations/routing`; “API” means the direct `/api/v1` endpoints. A scheduled event such as an interview is stored as a **task**. Atlas has no separate event, project, calendar, contact, or message entity.

| Entity | Operation | Conversation | Direct API |
| --- | --- | --- | --- |
| Task | Create with title, details, deadline | Yes; may include a linked reminder and note in one reviewed capture | `POST /tasks`, capture endpoints |
| Task | Read one, list, search, ask about details/status/deadline | Yes | `GET /tasks`, `GET /tasks/{id}`, `GET /search` |
| Task | Rename, change details/deadline, complete, reopen | Yes, one field and one record per turn | `PATCH /tasks/{id}`, `POST /tasks/{id}/complete` |
| Task | Delete one, or reviewed set of tasks and other record types | Yes; deletion requires confirmation | `DELETE /tasks/{id}`; no public batch endpoint |
| Task | Add/remove ordering dependency on another task | Yes, with exact pair selection and confirmation | `PUT/DELETE /tasks/{id}/before/{target}` |
| Reminder | Create one-time or repeating; optionally link to task | Yes, through capture; recurrence can be chosen when creating | `POST /reminders` |
| Reminder | Read one, list/search past or upcoming, inspect delivery/occurrence | Read/list/search yes; occurrence detail and delivery acknowledgement are not conversation actions | `GET /reminders`, `GET /reminders/{id}`, `GET /deliveries`, occurrence endpoint |
| Reminder | Rename, change time, complete, dismiss | Yes, one field and one record per turn | `POST /reminders/{id}/snooze`, `/complete`, `/dismiss`; no direct rename endpoint |
| Reminder | Change existing repeat rule | Yes, one reminder per turn; daily, weekly, or none | `PATCH /reminders/{id}` with `repeat`; current occurrence is preserved |
| Reminder | Change task link, acknowledge occurrence | Task link yes, with exact pair selection and confirmation; acknowledgement is not a conversation action | Task link and occurrence acknowledgement have direct endpoints |
| Reminder | Delete one or reviewed set | Yes; deletion requires confirmation | `DELETE /reminders/{id}`; no public batch endpoint |
| Note | Create body, optionally linked to a task | Yes, through capture | `POST /notes` also accepts a reminder link |
| Note | Read one, list, search, ask about content | Yes | `GET /notes`, `GET /notes/{id}`, `GET /search` |
| Note | Rename or replace body | Yes, one field and one record per turn | `PATCH /notes/{id}` |
| Note | Add/remove link to existing task or reminder | Yes, with exact pair selection and confirmation | `PUT/DELETE /notes/{id}/links/{kind}/{target}` |
| Note | Delete one or reviewed set | Yes; deletion requires confirmation | `DELETE /notes/{id}`; no public batch endpoint |
| Any two records | Add/remove “related to” relation; inspect relations | Add/remove yes, with exact pair selection and confirmation; relation listing remains a direct API action | `PUT/DELETE /relations/...`, `GET /relations` |
| Cross record | Create task with linked reminder/note in one transaction | Yes | Capture endpoints |
| Cross record | Delete a reviewed set of tasks, reminders, and notes | Yes, through conversation; checks record versions and linked active reminders in one transaction | Store operation only |
| Cross record | Batch create, edit, complete, or reschedule | No | No batch endpoint |
| Outside Atlas | Send email/message, modify calendar or external app, manage project/contact/event as a separate entity | No; explain the unavailable capability and invite a supported request | No |

## Conversation behavior checked in this PR

- A whole-collection deletion names only the supported kinds: tasks, reminders, and notes. Named or ambiguous selections go through a picker. Duplicate titles require explicit selection.
- A batch preview lists exact IDs, titles, kinds, and linked active reminder effects. Nothing is deleted until confirmation. The store rechecks versions and linked reminders inside one transaction; a changed review returns to selection or confirmation. A confirmation receipt is committed with the deletions so retrying after a lost response returns the same result.
- An explicit multi-action request is listed as ordered steps before any action starts. Each step uses the existing proposal or deletion review, and Atlas waits for “continue” before the next step. An unsupported step can be skipped visibly. Task captures with an attached reminder or note remain one atomic step.
- Existing-record note links, reminder task links, task dependencies, and record relations use exact pair selection. The selected pair and operation are saved with record versions before confirmation. The relationship write and replay receipt commit in one transaction; a changed record returns to selection.
- Unsupported requests explicitly say they cannot be done in the routing conversation and accept a follow-up. Unclear routing suggests the most likely actions when available. Invalid picker and pending-step replies receive an explanation rather than an unchanged silent response.

## Current limits

The natural-language router selects one primary action per step. The workflow parser recognizes explicit independent clauses; implicit multi-action phrasing can still need clarification. Bulk edits, relation listing, and occurrence acknowledgement are not conversation actions yet. Model classification can still be wrong for unusual phrasing; the reviewed proposal, exact target picker, and confirmation are the last checks before a write. A request with no matching saved record asks for a title or distinctive phrase. The record list shown by conversational lookup is capped at ten; use the direct list or search API for the full set.

Focused manual interfaces: `/batch-test`, `/workflow-test`, and `/association-test`. Relevant automated checks: `go test ./internal/httpapi ./internal/store ./internal/routing`.
