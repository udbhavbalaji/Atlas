# ADR-0001: The Three-Language Rule

**Status:** Accepted

**Date:** 2026-07-05

**Decision Makers:** Atlas Architecture Team

---

# Context

Atlas is intended to be a long-lived Personal Operating System.

Unlike many software projects, Atlas is expected to evolve over decades rather than years. During its lifetime, numerous technologies will emerge, mature, and disappear.

A common cause of architectural decay is uncontrolled technology adoption. New languages are introduced to solve isolated problems, resulting in:

- duplicated tooling
- fragmented expertise
- inconsistent coding standards
- increased onboarding cost
- incompatible deployment pipelines
- duplicated infrastructure
- unnecessary complexity

Atlas prioritizes architectural longevity over adopting the latest technology.

Therefore, the project intentionally constrains itself to a minimal set of programming languages.

---

# Decision

Atlas officially supports exactly **three implementation languages**.

| Language | Primary Responsibilities |
|-----------|--------------------------|
| Go | Core operating system, backend services, event bus, scheduler, plugin runtime, local APIs |
| TypeScript | User interfaces, shared models, browser extension, desktop application, mobile application, web dashboard |
| Python | Artificial intelligence, OCR, speech processing, embeddings, machine learning, advanced data processing |

No additional programming language may be introduced without an approved Architectural Decision Record (ADR).

---

# Rationale

The Three-Language Rule is not an optimization.

It is an architectural constraint designed to reduce long-term system complexity.

The rule optimizes for:

- maintainability
- shared engineering knowledge
- operational simplicity
- tooling consistency
- hiring efficiency
- documentation quality
- long-term sustainability

Every additional language permanently increases the cognitive load required to understand and maintain Atlas.

---

# Language Responsibilities

## Go

Go forms the operational foundation of Atlas.

Responsibilities include:

- Event Bus
- Scheduler
- Core Services
- Plugin Runtime
- Local API
- Background Workers
- Synchronization
- Storage Coordination
- Service Orchestration

### Why Go?

Go provides:

- excellent concurrency
- small static binaries
- fast startup
- cross-platform deployment
- straightforward tooling
- long-term ecosystem stability

Go is intentionally used where deterministic systems are preferred over AI-driven behavior.

---

## TypeScript

TypeScript is responsible for every primary user interface.

Responsibilities include:

- React Native application
- Desktop application
- Browser extension
- Web dashboard
- Shared API contracts
- Shared domain models
- Visualization
- Settings
- User interaction

### Why TypeScript?

Atlas presents a consistent experience across platforms.

TypeScript enables:

- shared types
- shared validation
- rapid interface development
- mature frontend ecosystems
- predictable developer experience

Business logic should remain in Go whenever practical.

User interfaces should primarily orchestrate rather than implement core behavior.

---

## Python

Python is reserved for computational workloads that benefit from its mature scientific and AI ecosystem.

Responsibilities include:

- OCR
- Speech recognition
- Embedding generation
- Semantic processing
- Machine learning
- Computer vision
- Model execution
- Experimental AI research

Python should expose stable APIs consumed by Go services.

Python should not become the primary orchestration language.

---

# Architectural Boundaries

Each language owns distinct responsibilities.

```
                User Interfaces
                     │
                     ▼
              TypeScript Layer
                     │
                     ▼
              Local Go Services
                     │
          ┌──────────┴──────────┐
          ▼                     ▼
   Deterministic Logic     AI Processing
          │                     │
          │                 Python Services
          └──────────┬──────────┘
                     ▼
               Event Bus
```

Languages communicate through explicit interfaces.

They should never rely upon implementation details of another language.

---

# Benefits

The Three-Language Rule provides:

## Predictable Architecture

Developers immediately understand where new functionality belongs.

---

## Reduced Maintenance

Fewer build systems.

Fewer package managers.

Fewer deployment pipelines.

Fewer security updates.

---

## Stronger Documentation

Documentation remains focused.

Examples remain reusable.

Engineering practices remain consistent.

---

## Easier Onboarding

New contributors need to learn only three ecosystems.

Knowledge transfer becomes significantly easier.

---

## Better Testing

Testing infrastructure remains standardized.

Integration testing becomes simpler.

Cross-language interactions become intentional rather than accidental.

---

# Trade-offs

The rule intentionally rejects some language-specific advantages.

For example:

- Rust may provide stronger memory guarantees.
- Swift may integrate more deeply with Apple platforms.
- Kotlin Multiplatform may reduce mobile duplication.
- C++ may offer maximum performance.

These benefits do not outweigh the long-term architectural cost of introducing another language.

Atlas optimizes for consistency over specialization.

---

# Exceptions

A new language may only be adopted if all of the following conditions are satisfied:

1. No existing language can reasonably solve the problem.
2. The new language provides substantial architectural benefit.
3. The maintenance cost is justified.
4. The benefit is expected to persist for many years.
5. A new ADR is approved.

Convenience is not sufficient justification.

Developer preference is not sufficient justification.

Popularity is not sufficient justification.

---

# Non-Goals

The Three-Language Rule does **not** prohibit:

- SQL
- Shell scripts
- JSON
- YAML
- Markdown
- Mermaid diagrams
- Protocol Buffers
- Configuration languages

These are considered supporting technologies rather than implementation languages.

---

# Consequences

Future architectural decisions should assume:

- Go owns system behavior.
- TypeScript owns presentation.
- Python owns advanced computation.

Whenever new functionality is proposed, the first design question should be:

> Which of the three languages naturally owns this responsibility?

If no clear answer exists, the proposed architecture should be reconsidered before implementation.

---

# Alternatives Considered

## Single Language

A single-language architecture simplifies tooling but would require compromising either user experience or AI capabilities.

Rejected.

---

## Best Tool for Every Job

Allowing unrestricted language adoption maximizes local optimization but significantly increases long-term architectural complexity.

Rejected.

---

## Periodic Technology Reevaluation

Regularly replacing implementation languages introduces unnecessary migration effort and destabilizes the platform.

Rejected.

---

# Related Documents

- `docs/philosophy/atlas-philosophy.md`
- `docs/architecture/system-vision.md`
- **ADR-0002 – Local-First Architecture**
- **ADR-0003 – Event-Driven Core**

---

# Guiding Principle

> **Architectural simplicity compounds over decades.**

Atlas intentionally limits technological diversity so that engineering effort is spent improving the system rather than maintaining its foundations.
