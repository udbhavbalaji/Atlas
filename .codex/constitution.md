# Atlas Constitution

**Version:** 1.0

**Status:** Active

**Last Updated:** 2026-07-05

---

# Purpose

This document defines the fundamental architectural principles that govern Atlas.

It exists to ensure long-term consistency across:

- Architecture
- Documentation
- Implementation
- Reviews
- Contributions
- AI-assisted development

This document has higher authority than implementation details.

When implementation conflicts with this constitution, the implementation should change.

---

# Mission

Atlas exists to reduce cognitive load while preserving human agency.

It is a local-first Personal Operating System that helps users understand, organize, and prioritize their lives without replacing their judgment.

Atlas does not make decisions.

Atlas prepares humans to make better decisions.

---

# Vision

Atlas is an event-driven Cognitive Operating System.

It continuously transforms observations into understanding and understanding into executive guidance.

The long-term objective is to provide the user with a continuously evolving understanding of their world while respecting privacy, attention, and autonomy.

---

# Architectural Principles

Every architectural decision should reinforce the following principles.

## Local First

Atlas should function without cloud services whenever practical.

Cloud capabilities are optional enhancements.

The local system is the source of truth.

---

## Human Agency

Atlas may:

- recommend
- organize
- explain
- challenge assumptions

Atlas must never:

- manipulate
- pressure
- shame
- remove meaningful choice

The human always commits.

---

## Explainability

Every recommendation should answer:

- Why?
- Why now?
- Based on what evidence?
- With what confidence?

Opaque reasoning is not acceptable.

---

## Privacy by Default

Atlas should disclose the minimum information necessary.

Information leaving the device must be:

- intentional
- explainable
- policy governed
- auditable

---

## Deterministic Before AI

Always prefer deterministic algorithms when they achieve equivalent results.

AI should be introduced only where it provides clear additional value.

AI is a dependency.

It is not the architecture.

---

## Event-Driven Architecture

Atlas communicates through immutable events.

Components should react to events rather than directly invoking one another whenever practical.

Events are historical facts.

They are never modified.

---

## Replayability

Derived state should be reconstructable.

Artifacts and events are the source of truth.

Knowledge, memory, indexes, and executive products should be reproducible.

---

## Modularity

Capabilities should be added through modules.

Kernel stability takes priority over convenience.

Avoid introducing unnecessary coupling.

---

## Vendor Independence

Components request capabilities rather than specific providers.

Example:

Good:

```
Summarization
```

Bad:

```
OpenAI GPT-5
```

The External Interaction Gateway determines providers.

---

# Architectural Layers

Atlas is composed of five primary architectural domains.

```
Kernel

Knowledge Platform

Cognitive Platform

Capability Modules

Interfaces
```

Each layer has a distinct responsibility.

---

# Kernel

The Kernel provides foundational platform services.

Examples include:

- Event Bus
- Scheduler
- Plugin Runtime
- Service Runtime
- Storage
- Configuration
- Search
- External Interaction Gateway

Kernel components expose stable contracts.

Kernel components should not depend on Capability Modules.

---

# Knowledge Platform

Responsible for transforming observations into structured understanding.

Includes:

- Artifact Store
- Knowledge Pipeline
- Context Graph
- Memory Engine

---

# Cognitive Platform

Responsible for transforming understanding into executive guidance.

Includes:

- Attention Engine
- Executive Engine
- Executive Products

---

# Capability Modules

Capability Modules extend Atlas with domain-specific behavior.

Examples:

- Responsibilities
- Research
- Finance
- Immigration
- Shopping
- Music
- Formula 1

Capability Modules should consume kernel contracts rather than modifying kernel behavior.

The term **Specialist** may be used in user-facing experiences.

The implementation term is **Capability Module**.

---

# Interfaces

Interfaces present Atlas to users.

Examples:

- Mobile
- Desktop
- Browser
- Voice
- API

Interfaces should remain thin.

Business logic belongs elsewhere.

---

# Cognitive Pipeline

Atlas processes information through the following conceptual pipeline.

```
Reality

↓

Capture

↓

Artifacts

↓

Knowledge Pipeline

↓

Context Graph

↓

Memory Engine

↓

Attention Engine

↓

Executive Engine

↓

Executive Products

↓

Human Decision
```

Each stage has one responsibility.

---

# Architectural Invariants

The following rules should rarely change.

## Evidence Before Understanding

Understanding must always be traceable to supporting artifacts.

---

## Memory Is Derived

Memory is not the source of truth.

Memory is derived from evidence.

---

## Attention Is Separate From Memory

Remembering something does not imply it deserves attention today.

---

## Executive Guidance Is Separate From Prioritization

The Executive Engine communicates.

The Attention Engine prioritizes.

---

## External Communication Is Mediated

No component may directly communicate with external providers.

All interactions pass through the External Interaction Gateway.

---

## Plugins Extend

Plugins extend Atlas.

They do not replace kernel behavior.

---

## Local Execution Preferred

Equivalent local capabilities should always be preferred over cloud execution.

---

# Documentation Standards

The repository is the canonical source of architectural truth.

Conversation history is not.

Documentation should remain internally consistent.

Cross references should be maintained.

Diagrams should remain synchronized with specifications.

---

# Specification Standards

Every specification should include:

- Purpose
- Philosophy
- Position in the Architecture
- Responsibilities
- Non-Goals
- Related Documents
- Guiding Principle

Specifications describe architecture rather than implementation.

---

# ADR Standards

Architectural Decision Records are historical documents.

They should not be rewritten.

If a decision changes:

- Create a new ADR.
- Mark the previous ADR as superseded where appropriate.

History should be preserved.

---

# Terminology

Use consistent terminology.

Preferred terms include:

- Artifact
- Event
- Evidence
- Context Graph
- Memory
- Attention Candidate
- Executive Product
- Executive Brief
- Capability Module
- Knowledge Processor
- External Interaction Gateway

Avoid introducing synonyms unless explicitly defined.

---

# AI Usage

AI is a tool.

It is not Atlas.

AI may assist with:

- OCR
- Entity extraction
- Summarization
- Semantic similarity
- Narrative generation

AI should never become the sole source of truth.

---

# Engineering Philosophy

Optimize for decades rather than weeks.

Favor:

- simplicity
- modularity
- replayability
- transparency
- observability
- explainability

Avoid:

- hidden state
- implicit behavior
- unnecessary abstraction
- premature optimization
- vendor lock-in

---

# Repository Maintenance

Before modifying documentation:

1. Read related specifications.
2. Read related ADRs.
3. Maintain terminology consistency.
4. Update diagrams if required.
5. Update cross references.
6. Preserve architectural coherence.

Repository consistency is more important than document completion.

---

# AI Contributor Instructions

When acting as an architectural contributor:

- Treat the repository as the canonical source of truth.
- Read relevant documentation before proposing changes.
- Maintain architectural consistency.
- Prefer additive evolution over disruptive redesign.
- Challenge proposals that violate this constitution.
- Explain trade-offs before recommending significant changes.
- Prefer stable interfaces over convenient implementations.
- Never optimize one subsystem at the expense of the overall architecture.

---

# Review Checklist

Before accepting any architectural change, ask:

- Does this reinforce local-first?
- Does this preserve human agency?
- Is AI actually necessary?
- Is the behavior explainable?
- Is the design event-driven?
- Is the state replayable?
- Does this introduce unnecessary coupling?
- Does this preserve modularity?
- Does this protect privacy?
- Can this architecture still scale in ten years?

If any answer is "no," the proposal should be reconsidered.

---

# Guiding Principle

> Atlas exists to amplify human judgment, not replace it.

Every feature, component, algorithm, and architectural decision should move Atlas closer to that goal.

The architecture should remain understandable, explainable, and trustworthy for years to come.
