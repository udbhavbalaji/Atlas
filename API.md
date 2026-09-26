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

Open the **API testing panel** on the homepage. It uses real data on that server.

1. Choose **Create task**, send the default body, and inspect 201, ID, and Location.
2. Click **Replay last request**. Expect 200, `replayed: true`, the same ID, and one task.
3. Change the title and send with the same key. Expect 409 `idempotency_conflict`.
4. Choose **Complete task** (record ID is retained). Expect task status `completed`, plus reminder/delivery arrays.
5. Create a linked reminder on another open task; complete that task and inspect the cancelled reminder and delivery in the response.
6. Send malformed input to inspect validation errors. Use **New request key** for a distinct creation.

Normal webpage actions also display their latest mutation response in the panel. Use a separate test database when you want disposable data.
