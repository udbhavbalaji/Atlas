# AGENTS.md

This repository contains the architecture and implementation of **Atlas**, a local-first Cognitive Operating System.

This document defines how AI coding agents and human contributors should work within this repository.

---

# Before You Make Changes

Before making any architectural, documentation, or implementation changes:

1. Read `.codex/constitution.md`.
2. Treat the repository as the canonical source of truth.
3. Read all related documents before editing.
4. Understand the existing architecture before proposing changes.

Never assume prior conversation context.

---

# Repository Philosophy

Atlas is a long-term engineering project.

Optimize for:

- correctness
- clarity
- modularity
- explainability
- maintainability
- long-term evolution

Avoid optimizing for short-term convenience.

---

# Source of Truth

The order of authority within this repository is:

1. `.codex/constitution.md`
2. ADRs (`docs/adr/`)
3. Architecture documents (`docs/architecture/`)
4. Specifications (`docs/specifications/`)
5. Implementation

If implementation conflicts with the documented architecture, raise the issue rather than silently changing the architecture.

---

# Working Principles

When modifying the repository:

- Preserve architectural consistency.
- Maintain terminology consistency.
- Update cross references.
- Update Mermaid diagrams when necessary.
- Keep documentation synchronized with implementation.
- Preserve replayability and explainability.

Every change should improve repository coherence.

---

# Architectural Decisions

Do not rewrite history.

If an architectural decision changes:

- Create a new ADR.
- Supersede older ADRs where appropriate.
- Preserve historical reasoning.

---

# Specifications

Specifications describe architecture.

They should not contain implementation-specific details unless explicitly intended.

Every specification should maintain:

- clear responsibilities
- clear non-goals
- related document references
- guiding principles

---

# Implementation

When implementing features:

- Prefer deterministic solutions before AI.
- Prefer event-driven communication.
- Prefer composition over inheritance.
- Prefer interfaces over concrete implementations.
- Prefer stable contracts over convenience.

Avoid unnecessary abstraction.

---

# Plugins

Capability Modules should extend Atlas.

They should not modify kernel behavior.

New functionality should integrate through existing platform contracts whenever possible.

---

# External Services

Atlas components must never communicate directly with external providers.

All external communication must pass through the External Interaction Gateway.

Components request capabilities, not providers.

---

# Documentation

Documentation is part of the product.

Whenever architecture changes:

- Update related specifications.
- Update diagrams.
- Update indexes if present.
- Update cross references.
- Identify affected ADRs.
- Ensure terminology remains consistent.

Avoid creating orphaned documentation.

---

# Code Generation

When generating code:

- Follow the documented architecture.
- Keep modules cohesive.
- Keep dependencies explicit.
- Avoid hidden behavior.
- Write self-explanatory code.
- Favor readability over cleverness.

If architectural assumptions are required, document them.

---

# Pull Requests

When preparing a change, include:

- Summary
- Motivation
- Architectural impact
- Documents updated
- ADR impact (if any)
- Follow-up work

---

# When Unsure

If a requested change appears to conflict with the architecture:

Do not silently implement it.

Instead:

1. Explain the conflict.
2. Identify affected documents.
3. Propose one or more architectural alternatives.
4. Wait for approval before making disruptive changes.

---

# Success Criteria

A successful contribution should:

- preserve local-first principles
- preserve human agency
- improve modularity
- improve explainability
- improve repository consistency
- remain understandable years from now

Every contribution should leave the repository in a better state than it was found.
