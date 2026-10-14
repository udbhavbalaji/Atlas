# Structured plan preview (experimental)

Follow-up to [PR #5's planning comment](https://github.com/udbhavbalaji/Atlas/pull/5#issuecomment-6123801070), based on merged `development` at `4e984f6`. PRs #3, #4, and #5 are not modified. This feature is **off by default** and has no executor.

## Existing decision flow

- `routingRoutes` configures the cached Jev decision evaluator and the existing argument planners. `evaluateRouting` is shared by stateless routing and durable conversations.
- In Jev mode, `evaluateJevPipeline` gathers bounded record context, selects a category and then one tool, and uses the existing language-model planner for arguments. The older direct `Jev.Evaluate` contract chooses one `primary_action`. Neither interface is a general multiple-action plan.
- `routingConversation` reuses its route during channel collection. `naturalCorrection` asks the same `routing.Evaluator` interface to choose a field; deterministic parsers and channel validation apply the value. Confirmation and the store's capture/linked mutation APIs own persistence.
- `provider.Provider.Propose` is a separate bounded proposal contract, with context/clarification/ready states. `provider.Run` validates its result and calls `CapturePreview`; commit is separate. The new planning package does not invoke these persistence or session interfaces.

## Jev protocol and scope

The [OpenRouter Decisions API](https://openrouter.ai/docs/api/api-reference/alphadecisions/submit-a-decisions-request) supports `choice`, `noul`, and `score` questions, **not arbitrary generated JSON objects**. `Jev.EvaluateQuestions` sends supported choice questions together in one request. `Jev.Evaluate` reuses that transport and preserves its existing single-choice result.

Atlas splits the source into at most six clause candidates and supplies three questions per clause: capability, timing presence, and task-deadline reference. A coverage question checks that the candidate split faithfully represents the request. Go assembles a typed plan from validated decisions and source text. There is no second extraction model, routing pass, automatic retry, or automatic fallback. Preview failures offer the existing conversation UI as an explicit alternative.

This first version supports `task.create`, `reminder.create`, and `note.create`, including multiple actions of the same type. Other capabilities become explicit questions. Action IDs are stable within a plan. Arguments are typed; unresolved fields appear in `issues`, keyed by action ID and field. References identify an action, `due_at`, and an integer minute offset. Unknown capabilities, duplicate IDs, invalid references, cycles, argument mismatches and ungrounded values are rejected.

All text arguments remain verbatim source substrings after removing a supported command prefix. Titles may retain timing words; this version does not paraphrase them. Relative reminder offsets support one explicit numeric or one-through-twelve minute/hour offset (and half an hour) before/after a task deadline. Unrecognized or ambiguous dependencies require clarification. Absolute times require a complete RFC3339 instant with offset. Date-only, local wall-clock/DST, ambiguous numeric dates, and natural-language times remain unresolved. The supplied IANA timezone is validated. No clock time, date, timezone or title is silently invented. The linked acceptance fixture preserves `tomorrow` and `-120` minutes, with questions for the task deadline and dependent reminder.

Clause splitting is deliberately bounded, not general linguistic decomposition: commas, semicolons, newlines, “and”, and “then” create candidates. Compound titles, quoted content and conjunctions inside notes can need rephrasing. Coverage or low-confidence decisions cannot produce a ready plan. This limitation is visible in the testing UI and blocks broad rollout until live evaluation supports it.

## Preview and revisions

Start a separate test server:

```sh
ATLAS_PLAN_PREVIEW=1 go run ./cmd/atlas -db /tmp/atlas-planning.db -addr 127.0.0.1:8081
```

Open `/planning-test`. It defaults to **mock fixtures** (zero model calls). Choose live Jev explicitly to use the configured OpenRouter key. A preview makes one request containing at most 19 questions. The page displays actions, references, issues, revision number, JSON and call accounting, with no save control.

- `GET /api/v1/planning`: flag, capabilities, limits and existing-flow link.
- `GET /api/v1/planning/fixtures`: named fixed mock cases; not a natural-language mock parser.
- `POST /api/v1/planning/fixtures/{name}` with `{}`: zero-call fixture preview, gated by the feature flag.
- `POST /api/v1/planning/preview` with `{"text":"...","timezone":"Asia/Kolkata"}`: one Jev request. The server assigns the reference instant.
- `POST /api/v1/planning/revise` with `{"preview_token":"...","action_id":"a1","field":"due_at","value":"2026-10-13T18:00:00+05:30"}`: deterministic field correction, no inference.
- Alternatively pass `{"preview_token":"...","text":"Change the task deadline to 2026-10-13T18:00:00+05:30"}`: one choice selects action+field using the conversation's `routing.Evaluator` boundary and existing explicit-value grammar. The existing decision cache reuses identical natural corrections. Unclear or compound corrections do not change the plan.

Responses include `plan`, `issues`, `ready`, `preview_only:true`, `persisted:false`, `model_calls`, and a signed `preview_token`. `ready` means the preview passed the current validators; it does not authorize execution. Tokens expire after 30 minutes, are local to a server instance, and cannot be used as routing or capture receipts. Revisions are stateless branches from the supplied preview; there is no durable conversation or global “latest revision”. Only the targeted action changes. A reminder's symbolic deadline reference survives task changes; explicitly changing that reminder's scheduled time removes only its own reference.

No store is passed to the preview service. No task, reminder, note, relation, session, activity, proposal or commit record is written. There is no `/planning/commit` endpoint or conversion to the existing dispatch/commit interfaces. Existing routing, conversations, confirmation and persistence behavior remain the default.

## Verification and rollout

`go test ./...` uses fixed decisions and simulated HTTP. It covers the single-action baseline comparison, independent actions, repeated capabilities, linked acceptance case, ambiguity, targeted corrections, low confidence, required fields, malformed output, invalid references, cycles, no retry, feature flag, receipt separation, no record writes and unchanged siblings. Mock baseline choices are illustrative fixtures, not measured Jev performance.

Real Jev checks are opt-in, bounded and excluded from ordinary tests:

```sh
# Explicitly provide a key through the environment. One fixture, two calls:
ATLAS_LIVE_PLAN_FIXTURE=linked go test ./internal/planning -run '^TestLiveJevPlanningComparison$' -v -count=1
```

Choose `single`, `independent`, `linked`, `ambiguous`, or `two_tasks`. Each run compares one batched preview against one current single-primary-choice router call on the same text and reference instant, logs the result/usage, and checks expected capabilities/readiness. This does not pretend to benchmark the complete production category/tool/extraction pipeline. Malformed model output is injected locally, never induced through a paid live call. Natural correction can be evaluated separately with `ATLAS_LIVE_PLAN_CORRECTION=1` and `-run '^TestLiveJevPlanCorrection$'` (one call using the linked fixture as the existing preview).

Keep the flag off by default. Before an opt-in pilot, require all deterministic tests and the acceptance fixture to pass, all five live semantic fixtures and the natural correction check to preserve the intended actions, no invented times, no sibling changes, and no persistence. Record latency and usage, and verify upstream failure remains explicit. Before broader rollout, add representative phrasing and adversarial/ambiguous cases and require every unsupported split to clarify instead of silently dropping or adding actions. Any unsafe-ready result blocks rollout. A separate reviewed design/PR is required for execution; changing the flag must never enable it.
