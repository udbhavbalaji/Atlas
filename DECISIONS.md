# Atlas Decisions

## iPhone client and distribution

**Status:** Accepted

**Decision:** Atlas will target iPhone first, with a responsive Progressive Web App (PWA) as the initial client. The app must be usable by the owner without requiring a paid Apple Developer Program membership.

### Rationale

- A PWA can be used from iPhone Safari and added to the Home Screen without a paid developer account.
- It avoids the recurring provisioning burden of a free Xcode Personal Team installation.
- The Atlas API remains independent from the client, allowing a native client to be added later without moving business logic into the app.

### Initial client scope

The iPhone PWA should support:

- Text capture and request submission
- Task, note, and reminder management
- Activity and decision history
- Offline drafts and queued requests where practical
- Responsive layouts designed specifically for iPhone Safari

Voice capture should follow the text workflow rather than precede it.

### Distribution alternatives considered

- **Native SwiftUI app with Xcode Personal Team:** viable for personal testing, but free provisioning is limited and installations expire periodically, requiring rebuilds and reinstallation.
- **App Store distribution:** requires paid Apple Developer Program membership.
- **Alternative app distribution:** region-dependent and requires additional developer-side setup, so it is not the initial path.

### Consequences

The first version should avoid relying on native-only capabilities. Background execution, push notifications, and advanced voice behavior may be more limited in a PWA and should be validated with the first vertical slice. If those limitations become blocking, Atlas can add a thin native SwiftUI shell while retaining the existing API and domain logic.

## Webpage interfaces for feature testing

**Status:** Accepted

**Decision:** Every Atlas feature must include a webpage interface through which the owner can manually test its behavior. Extend the existing task interface where appropriate, or add a focused testing page for capabilities that need their own controls.

### Consequences

- Include usable inputs, visible committed results, validation messages, and failure states with each feature.
- Keep the webpage connected to the real Atlas API and persistence, so manual tests exercise actual functionality.
- Document a short manual test flow and verify the interface before considering a feature complete.
- Browser testing supplements automated tests for migrations, durability, scheduling, policy, and other core behavior.
- Keep canonical business logic in the core; testing pages can evolve into the product interface.

## Task deadlines and reminder times

**Status:** Accepted

**Decision:** Task details and optional deadlines are implemented before reminders. A task deadline and a notification time are separate values. Setting a deadline does not implicitly schedule a reminder.

The first deadline interface takes an explicit local date and time, displays the browser timezone, and stores the corresponding UTC instant. Open tasks past that instant are overdue. Completed tasks are not overdue. Deadlines and details can be edited or cleared. Date-only deadlines, recurrence, notifications, and snoozing will be added in subsequent milestones with explicit scheduling semantics.

## First reminder delivery channel

**Status:** Accepted

**Decision:** Prove fixed-time scheduling through a durable webpage reminder inbox before introducing desktop or iPhone push providers. The scheduler publishes inbox state, delivery state, and activity atomically. Delivery records survive restart; an already delivered occurrence is not published again. Snooze creates a fresh occurrence. Failed transactions are retried on the next scheduler tick.

**Consequences:** Atlas runs the scheduler while the service is running and catches up after downtime. The webpage displays due, upcoming, dismissed, and completed reminders, snooze/reschedule controls, delivery history, and a five-second test action. This milestone implements standalone one-time reminders; task links, recurrence, and external notification delivery come later. A future external provider will need its own attempt/error tracking, bounded retry policy, and deduplication contract.

## Task-linked reminders

**Status:** Accepted

**Decision:** An open task may have multiple explicitly linked reminders, independent of its deadline. Task completion or deletion cancels active linked reminders and queued deliveries in the same transaction as the task change, acknowledges delivered inbox entries, and records the reason in activity history.

**Consequences:** Completing a reminder does not complete its task. Reopening a task does not revive old reminders; the user can add new ones. Reminder records retain the task ID and a title snapshot after task deletion so delivery and cancellation history remain inspectable. Standalone reminders continue independently. Task links are validated against canonical task state when reminders are created. The scheduler also checks linked task state before delivering. The webpage includes per-task creation/testing controls, linked reminders with snooze/completion actions, and links from reminders back to their tasks.

## Structured API state and retry-safe creation

**Status:** Accepted

**Decision:** Task and reminder actions return canonical structured state captured inside the mutation transaction and returned after commit. Errors expose stable codes, readable messages, and a retryable flag. OpenAPI schemas document the wire contract. Task and reminder creation accept optional durable idempotency keys, with receipts committed alongside records and activity.

**Consequences:** Contract revision 2 replaces empty action responses and string errors; clients must update parsing. Creation replay returns the original creation response even after later edits or deletion, so clients fetch current state separately. Keys are scoped per creation operation and retained without automatic expiry. Other mutations do not use receipts. The webpage provides endpoint selection, raw JSON input, response inspection, and exact request replay for manual testing. Recurrence follows this API strengthening pass.

## Calendar recurrence and occurrence acknowledgment

**Status:** Accepted

**Decision:** Begin recurrence with daily and weekly reminders at the original local clock in an IANA timezone. Keep one active occurrence, awaiting user acknowledgment before queuing the next future repeat. Complete/dismiss occurrence is distinct from stopping the series. Occurrence IDs provide durable retry deduplication.

**Consequences:** Downtime surfaces one overdue occurrence; acknowledgment skips missed repeats. Snooze preserves the series clock. Missing local times are skipped and ambiguous times choose the earlier instant. Explicit first timestamps are honored. Task completion/deletion stops linked repeats, and reopening does not revive them. Creation receipts include the repeat rule. The webpage includes repeat selection, five-second testing, occurrence actions, stop controls, visible repeat clocks, and API inspection. Monthly/custom rules, end dates, editing rules, and task recurrence remain deferred.

## Persistent plain-text notes with explicit context links

**Status:** Accepted

**Decision:** Notes store plain text and explicitly link to existing tasks and reminders. One note may have multiple targets, including completed records. Note mutations, link validation, creation receipts, and activity commit transactionally. Task and reminder state responses expose linked notes.

**Consequences:** Task deletion preserves note content and link provenance, using the title snapshot from attachment when the target no longer exists. Note deletion removes links but preserves activity and creation receipts. Duplicate linking/unlinking is a no-op. Note text is not copied into activity entries. The webpage provides capture, editing, deletion, linking/unlinking, per-record linked note views, and API testing. Search, rich text, attachments, revision history, and a general relation graph are deferred.

## Direct reminder task links in both creation flows

**Status:** Accepted

**Decision:** Expose direct task selection in the general reminder creation form and allow active reminders to attach, change, or remove their task link. Keep Add reminder on task cards. Notes are independent context links and do not establish task-reminder lifecycle relationships.

**Consequences:** Targets must be open tasks. Schedule, recurrence, deliveries, and note links remain intact during attachment changes. Completed/cancelled reminders retain their link provenance. Repeated identical changes create no duplicate activity; changed links record activity atomically. The webpage and API testing panel expose these controls.

## Focused feature tabs for manual testing

**Status:** Accepted

**Decision:** Organize the testing webpage into Tasks, Reminders, Notes, Activity, and API lab tabs, with a dark minimalist theme. Each feature includes a concise testing checklist, and only its current panel is visible. API inputs are shown when relevant to the selected endpoint.

**Consequences:** New features must fit an existing feature tab or add a focused tab and manual test guide. Preserve form drafts when switching tabs; URL fragments identify the active panel. Cross-record shortcuts switch panels before navigating. Due reminders remain discoverable through a navigation badge. Tabs support keyboard navigation and phone layouts. This changes presentation without changing API or persistence semantics.

## Canonical text search with a focused testing tab

**Status:** Accepted

**Decision:** Search task titles/details, reminder titles, and note bodies with literal Unicode lowercase substring matching. Read canonical tables in one SQLite snapshot; return bounded pages of structured results with matched fields, snippets, and webpage/API record links. Add a dedicated Search testing tab and API lab endpoint.

**Consequences:** No migration or duplicate index state is required. Type/status filters and strict query validation keep API behavior explicit. Results are newest first; pages may shift between requests after writes. The initial scan targets personal datasets. Indexed full-text search, ranking, stemming, and semantic search are deferred. Record links reveal completed tasks and reminder history and survive reloads.

## Explicit unified capture with atomic confirmation

**Status:** Accepted

**Decision:** Add a Capture testing tab and preview/commit APIs for one task and an optional directly linked reminder. Start with explicit validated fields. Preview has no persistence effects, exposes normalized input, planned effects, and warnings, and identifies content with a versioned fingerprint. Confirmation requires unchanged content and a durable retry key; records, delivery, activity, and receipt commit together.

**Consequences:** Browser edits invalidate preview. Uncertain confirmation preserves its exact request in session storage and locks editing until retry succeeds. A fingerprint identifies content and does not enforce authorization; clients own user confirmation. Existing lifecycle cancellation, recurrence, and receipt semantics apply. No migration is required. Natural-language interpretation, saved drafts, note capture, and multi-step general orchestration remain future work.


## Notes and standalone kinds in atomic capture

**Status:** Accepted

**Decision:** Extend capture with kind (task/reminder/note) and note_body. Notes in action captures explicitly link to all newly created task/reminder records. Reuse transactional note creation and commit records, links, activity, queued delivery, and receipt together. Preserve the existing capture response shape, with null task/empty task_id for standalone kinds.

**Consequences:** Note changes invalidate the content fingerprint. Legacy task-only previews/receipts remain compatible. Capture exposes note/type fields, raw proposal/state, and links to every created record. No migration is needed.

## Local sentence interpretation as a proposal step

**Status:** Accepted

**Decision:** Add a local English rule interpreter ahead of capture validation. Supported sentences yield explicit proposals; ambiguous or unsupported timing yields structured clarification questions and an editable draft. Require user review and the existing atomic confirmation flow. Never save from interpretation alone.

**Consequences:** Relative times resolve once against server reference time and the browser's IANA timezone. Assumptions, exact dates, recurrence, and note targets are visible. DST gaps and overlaps require clarification rather than normalization. Known English capture forms work offline from external reasoning services; generic semantic understanding, multilingual input, batches, existing-record commands, and opt-in model adapters are deferred. The Capture tab and API lab expose sentence input, example phrases, questions, editable fields, and persisted results for manual testing.

## Explicit task context and prerequisite ordering

**Status:** Accepted

**Decision:** Resolve before/ahead-of clauses against open task titles using local token matching. Expose candidate tasks and a visible context selector for pronouns such as “before that.” Save directed prerequisite edges independently of notes or reminders. Preview binds the referenced task snapshot; atomic confirmation rechecks its version and open status. Existing creation receipts remain replayable after context changes or deletion.

**Consequences:** Schema migration 8 adds durable dependency edges. Cycles are rejected; duplicate attach/remove operations are idempotent. Task snapshots expose both directions, current statuses, existence and satisfaction; deleted endpoints retain title provenance. Ordering is informational and does not block completion or invent deadlines/delivery times. Relative offsets from another task and conversational memory remain deferred. Capture, Tasks and API lab provide testing controls for lookup, selection, direct linking, unlinking, completion state and raw structured responses.

## Related task suggestions and reviewed reminder lead times

**Status:** Accepted; extends the explicit task-context decision above.

**Decision:** Reminder captures suggest context through meaningful shared title tokens and a small explicit resume/interview vocabulary bridge. Multiple matches require selection. A visible lead-time control proposes a reminder before a dated context deadline (default one hour); supported explicit minute/hour/day/week offsets override the default. Contextual reminders create a prerequisite task with a directly linked reminder. Show identified context even when its deadline is missing.

**Consequences:** No migration is required. Interpretation remains a local heuristic, with user review and editable relationships. Undated tasks explain missing timing rather than generic missing context. Past derived times, unsupported offsets, DST gaps/overlaps and competing time requests require clarification. Preview binds the target snapshot and commit rechecks it. Delivery times are fixed after saving; live relative rescheduling remains future work. Capture exposes lead-time selection, context, candidate choice, computed times, explanations, explicit fields and raw state for manual testing.

## Common time idioms with visible defaults

**Status:** Accepted

**Decision:** Normalize complete common time idioms into the existing strict time parser. Support local calendar boundaries (day/week/month), close of business, named day periods and fractional/couple intervals. Expose conventional clock defaults in assumptions and a collapsible Capture phrase guide; include an end-of-day movie-ticket example button.

**Consequences:** No migration is required. End of day is 23:59, week ends Sunday, close of business is 17:00 without holiday logic; morning/afternoon/evening/night defaults are 09:00/15:00/18:00/20:00. Exact clocks, timezone, DST validation, capture context, preview and atomic commit semantics remain in use. Passed named times and conflicting or unrecognized suffixes require clarification. General linguistic understanding and vague phrases such as “a few hours” remain unsupported.

## Typed capture continuations for future orchestrators

**Status:** Accepted

**Decision:** Add an additive, versioned continuation envelope to interpretation APIs. Describe clarification IDs, answer value types, choices, request fields, preset values, follow-up calls, confirmation requirements and endpoint/body templates. Preserve legacy questions and existing capture validation.

**Consequences:** Consumers need not parse English prompts to choose context or supply timing. Read-only interpretation/preview remain separate from confirmed mutations. Reference deadline edits explicitly require confirmation and re-interpretation. Templates do not grant authorization. Capture exposes the full interpretation/continuation JSON for testing; no migration or automatic dispatch is introduced.

## Generic symmetric record associations

**Status:** Accepted

**Decision:** Add `related_to` associations between any two distinct tasks, reminders or notes. Canonicalize endpoint order and derive a deterministic resource ID so PUT/DELETE are idempotent across reversed pairs. Return committed endpoint snapshots and support record-scoped reads. Keep association semantics independent of reminder ownership, prerequisite ordering and note links.

**Consequences:** Schema migration 9 adds association storage and relation activity IDs. Deleted endpoints retain title provenance with explicit missing status; associations can still be removed. No scheduling or lifecycle side effect is implied by association. Relation changes and activity commit together. A focused Relations testing tab supports creation, filtering, record navigation, removal, raw snapshots and mutation responses. Generic relation types beyond related_to and graph traversal remain deferred.

## Durable conversational capture (2026-09-28)

Capture conversations wrap the existing bounded local interpreter with durable SQLite history and optimistic versions. Natural-language replies resolve task context and timing; unsupported/ambiguous replies preserve unresolved meaning. Original requests keep their reference time during context selection. An orchestrator can create, fetch, and reply through structured session endpoints. Each response carries canonical session state and the exact next request shape.

Explicit confirmation freezes the reviewed draft before atomic capture commit. A stable session receipt key recovers interrupted confirmation without duplicate action records. Changed dependency versions require review and confirmation again. Conversation history is durable before confirmation; action records are not. No automatic edits to reference tasks or cross-session memory are inferred.

The feature has a Conversation test section in Capture, visible history, proposal/assumption display, context choices, resume-by-ID, and structured JSON. The former capture tools remain available in a collapsed section. Every Atlas feature continues to require its own testable webpage interface.


## Jev owns decisions from user input

**Status:** Accepted — target architecture locked by user decision (2026-09-27).

**Decision:** Use Jev to interpret user input and make semantic decisions: intent, whether tasks/reminders/notes are required, which existing records supply context, and which clarification to request. Do not bake these decisions into a growing set of programmatic language rules in Atlas.

Atlas owns canonical state, bounded context retrieval, versioned structured proposal/session APIs, schema validation, date/capability checks, permissions, confirmation, transactions, idempotency, and delivery. Jev proposes operations through those boundaries; it does not write directly to storage. User confirmation remains required before committing a capture.

**Consequences:** The local English interpreter and reply matcher are interim compatibility mechanisms, not the long-term decision engine. This supersedes earlier decisions treating local rule-based semantic interpretation as the intended architecture. Complete narrowly scoped fixes already underway, then prioritize the bounded Jev adapter rather than broader phrase-rule expansion. Keep deterministic handling for explicit structured requests and validation. Verify Jev's actual interface and deployment requirements before integration; this decision does not claim that Jev is connected or change the current runtime.

## Offer optional reminder and note enrichment before task confirmation

**Status:** Accepted; pending implementation through the Jev conversation flow.

**Decision:** When capturing a new task, offer an optional reminder and an optional attached note before final confirmation. Provide a clear “save task only” path; neither addition is mandatory. If the user's request already supplies a reminder or note, show it for review and ask only about missing additions, without repeating questions already answered. A reminder requires an explicit or reviewed resolved time; do not infer an arbitrary time solely because the user chose to add one.

Jev owns interpretation of replies and selection of focused conversational questions. Atlas exposes versioned structured choices and validated draft fields for task/reminder/note enrichment. Session history retains answers and skipped options across reloads. Editing additions requires a refreshed proposal; one explicit confirmation atomically saves the chosen records and links. Existing-record edits or extra reminders are separate operations, never implicit effects of this offer.

**Verification:** Add webpage conversation controls and tests for task-only skip, reminder timing, note attachment, already-supplied additions, resume, and duplicate-safe confirmation. This is a product-flow requirement, not a new rule-based English decision engine.

## Provider-neutral foundation while Jev access is pending

**Status:** Accepted; mock implemented, Jev transport pending.

**Decision:** Define Atlas's own versioned provider interface and structured statuses (`needs_context`, `needs_clarification`, `ready`). A provider proposes capture fields or focused questions; Atlas validates the contract, resolves bounded context, validates canonical references and fields, and returns a reviewed proposal. It never commits from a provider response alone. The future Jev adapter translates its verified real API into this internal contract; no Jev transport, endpoint, SDK, credentials, or behavior is assumed.

The mock is explicitly fixture-driven and does not interpret English. It exercises task-only skip, optional reminders/notes, context selection, malformed output, and unavailability. Context is limited to one round, one query, and five open tasks, exposing only ID, title, deadline, and updated_at. References outside supplied context are rejected. Provider calls receive a five-second deadline; adapters must honor cancellation. Provider failure never silently falls back to a different interpreter or creates records.

**Consequences:** A Providers testing tab shows mock availability, requests/context/responses, validation errors, optional fields, preview, and explicit confirmation through existing capture commit and receipts. Mock questions/answers are stateless server-side and resubmitted explicitly; this is an integration harness, not the live Capture conversation's decision engine. Pending confirmation survives same-tab reload via session storage. Existing local Capture continues as the temporary bridge. Durable provider conversations and the actual Jev adapter remain next work once its interface is available. No database migration is introduced.

## Jev routes input; Atlas channels own their workflows

**Status:** Accepted by the user (2026-09-27). Routing foundation implemented. The Vercel transport below was superseded by the OpenRouter decision below.

**Decision:** Send user input and bounded relevant context to Jev with an extensible registry of implemented actions. Return the primary-action distribution, selected channel and explicit uncertainty state. The orchestrator validates and routes that result; the selected task, reminder or note subsystem owns subsequent field collection, optional enrichment, context requests and validated proposals. Register future capabilities together with their handlers. A primary Choice is a distribution over competing routes, not independent evidence that multiple records must all be created. Independent multi-action requests can require clarification; linked additions belong to the selected channel.

Jev is an evaluation model, not a free-form generator. It selects among supplied options; it does not generate arbitrary task titles, note bodies or timestamps. This refines earlier wording that Jev directly drafts operations: the adapter/orchestrator composes decisions and channel output into Atlas's proposal contract. The first routing channel implementation uses explicit reviewed fields. It does not call the interim English interpreter or claim that extraction is complete. Live Capture conversations continue to use the interim interpreter until a real extraction/channel-decision adapter is connected.

**Historical transport (superseded):** The initial adapter used the TypeSafe-compatible Vercel HTTP API (`/typesafe/v1/systemone`, model `typesafe-ai/jev`). Live synthetic verification returned HTTP 403 `customer_verification_required`: Vercel required a card on file even for included credits on this account. The user did not want to add a card. Gemini would be a different model, not a Jev host, and was not enabled implicitly.

**Call budget:** One initial Choice evaluation by default. Dispatch, field edits, additions, preview, reload and confirmation do not call Jev. Cache validated successful routing evaluations for five minutes with up to 128 entries, using hashes of input, timezone, registry criteria and supplied record snapshots. Coalesce concurrent identical calls. The routing reference clock is excluded only because the cache decides primary intent, never dates. Changed record snapshots, input or registry criteria require fresh evaluation. Failures are not cached; there are no automatic model retries or fallback providers. Report cache hits, originating token usage/cost and model calls for each request. Automated tests use fixtures and simulated HTTP; live checks are small and explicit.

**Boundaries:** Routes require valid complete probabilities and a selected maximum. Initial routing thresholds (0.65 probability and 0.15 margin) are provisional and visible, not an accuracy guarantee. Unknown/uncertain intent requires user review. A signed, expiring routing receipt binds source/context/route and avoids repeated inference during channel collection. It expires after 30 minutes or a server restart; pending confirmed captures retain the existing durable storage receipt behavior. Routing and dispatch do not persist action records. Final confirmation goes through existing atomic capture validation and idempotency.

**Testing UI:** Every feature retains a webpage testing interface. Routing has its own dark minimal tab showing provider configuration, registry, input/context, probabilities, channel questions, optional reminders/notes, structured responses, preview and explicit confirmation. Mock mode is clearly fixture-driven. Live access and natural-language extraction are not represented as working when they are unavailable. No migration is introduced.

## OpenRouter transport for Jev routing

**Status:** Accepted by the user (2026-09-28); supersedes the Vercel transport above. No promotion to `main` without explicit approval.

**Decision:** Use OpenRouter's Decisions API at `POST /api/alpha/decisions` with pinned model `typesafe/jev-1.13` for the existing one-question Jev routing contract. Send structured Atlas state and action criteria; validate the complete returned distribution before selecting a channel. Read the server-side `OPENROUTER_API_KEY` or ignored `data/openrouter.key`; the old Vercel key is inactive. Surface token use and `usage.cost` in the routing result, and retain the five-minute successful-result cache. No automatic retries, provider fallbacks, or extra model calls during dispatch and confirmation. Live verification uses one minimal synthetic sentence without stored context.

## Main promotion requires explicit user approval

**Status:** Accepted (2026-09-27); supersedes autonomous milestone promotion.

**Decision:** Branch new work from `development` onto feature branches and merge tested work back into `development`. Never merge into `main` autonomously. At a substantial milestone, present the completed scope, verification evidence and remaining risks/limitations and recommend whether to promote. Merge `development` into `main` only after the user explicitly approves that promotion. Prior general authorization to implement features does not authorize main promotion.

## Folded Path is the Atlas identity for now

**Status:** Accepted by the user (2026-09-28); provisional until a later explicit brand decision.

**Decision:** Use the Folded Path ribbon mark as the native iPhone app icon and the standalone Atlas symbol. Pair it with an `ATLAS` wordmark where space allows. Replace the prior mint `A.` icon and use the same symbol in the testing webpage header and favicon. Keep the vector masters, iPhone asset catalog, and webpage copies aligned. The palette and asset locations are recorded in [brand/README.md](brand/README.md).

**Verification:** The icon export must be a full-bleed, opaque 1024 × 1024 RGB PNG. Visual checks should cover the icon at app size and the logo on light and dark backgrounds. Device build and installation remain separate from this brand decision.

## Carry routing context into channel preparation

**Status:** Accepted (2026-09-27).

**Decision:** Preserve the signed routing state through channel dispatch and expose it in channel responses and the secondary action form. The original sentence, timezone, frozen reference clock and supplied minimized task records are tool input, not discarded routing-only metadata.

Offer editable initial field suggestions using the existing local parser as a temporary extraction helper; its intent never overrides the chosen route. This refines the initial explicit-only channel implementation without expanding language rules or adding model calls. Show provenance, assumptions and unresolved timing. Initial preparation cannot return a confirmable proposal; user review and a subsequent explicit preview remain required. Normal field edits/clears are never reseeded. Existing-record context is visible and can be explicitly copied into relevant controls or selected as a prerequisite target; do not silently inherit deadlines or create relationships merely because a search returned one record. Keep exact instants when round-tripping prefilled/copied dates.
