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
