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
