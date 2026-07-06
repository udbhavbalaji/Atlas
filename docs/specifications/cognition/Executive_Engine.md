# Executive Engine Specification

**Document ID:** SPEC-0009

**Version:** 1.0

**Status:** Accepted

**Last Updated:** 2026-07-05

---

# Purpose

The Executive Engine is responsible for transforming prioritized understanding into structured, explainable executive products.

Its primary role is composition.

It receives ranked attention candidates, contextual understanding, and supporting evidence, then assembles coherent briefings appropriate for a specific situation.

The Executive Engine does not decide what deserves attention.

That responsibility belongs to the Attention Engine.

---

# Philosophy

The Executive Engine communicates.

It does not think.

It does not remember.

It does not prioritize.

It explains.

Its success is measured by how clearly it communicates Atlas's understanding to the user.

---

# Position in the Architecture

```mermaid
flowchart LR

ContextGraph --> MemoryEngine
MemoryEngine --> AttentionEngine
AttentionEngine --> ExecutiveEngine
ExecutiveEngine --> ExecutiveProducts
ExecutiveProducts --> User
```

---

# Responsibilities

The Executive Engine is responsible for:

- Brief composition
- Section generation
- Explanation generation
- Evidence linking
- Narrative organization
- Duplicate suppression
- Tone consistency
- Product formatting

It is **not** responsible for:

- Knowledge extraction
- Prioritization
- Memory formation
- AI orchestration
- External actions

---

# Executive Products

The Executive Engine produces multiple product types.

Examples include:

- Daily Executive Brief
- Weekly Review
- Monthly Review
- Quarterly Planning Brief
- Meeting Brief
- Travel Brief
- Immigration Brief
- Project Brief
- Research Brief
- Shopping Brief
- Voice Summary

Future products should reuse the same composition model.

---

# Inputs

The Executive Engine consumes:

- Ranked attention candidates
- Context Graph references
- Memory annotations
- Supporting evidence
- User preferences
- Product template
- Current context

It never queries raw artifacts directly.

---

# Output Model

Every executive product consists of:

```text
Executive Product
├── Metadata
├── Sections
├── Brief Cards
├── Explanations
├── Evidence Links
└── Suggested Options
```

---

# Composition Pipeline

```text
Attention Candidates

↓

Grouping

↓

Deduplication

↓

Section Assignment

↓

Brief Card Creation

↓

Narrative Composition

↓

Evidence Attachment

↓

Executive Product
```

Each stage has a single responsibility.

---

# Grouping

Related candidates should be grouped.

Example:

Instead of:

- Deadline A
- Deadline B
- Deadline C

Produce:

```
Three project deadlines this week
```

The user should see concepts rather than noise.

---

# Deduplication

The Executive Engine removes repeated information.

A recommendation appearing in multiple domains should be presented once with appropriate context.

---

# Section Assignment

Brief Cards are organized into logical sections.

Examples:

- Critical Items
- Opportunities
- Projects
- Relationships
- Finance
- Learning

Sections may vary between executive products.

---

# Brief Cards

The Brief Card is the fundamental presentation unit.

Every card includes:

- Identifier
- Title
- Summary
- Why it matters
- Why now
- Supporting evidence
- Confidence
- Suggested options
- Related entities
- Related artifacts

Cards should be self-contained.

---

# Narrative Composition

The Executive Engine determines ordering and transitions between sections.

Its purpose is readability.

It should avoid presenting isolated facts when a coherent narrative provides greater clarity.

---

# Explanations

Every recommendation must answer:

- Why is this here?
- Why now?
- What supports it?
- What are my options?

Explanations should remain concise and evidence-backed.

---

# Human Agency

Recommendations should never imply obligation.

Preferred language:

- You may wish to…
- Consider…
- One option is…

Avoid:

- You must…
- You should…
- You need to…

Unless communicating objective constraints (e.g., legal deadlines).

---

# Confidence

Each card exposes confidence separately from priority.

High priority does not imply high confidence.

Low confidence recommendations should clearly indicate uncertainty.

---

# Evidence

Every card should reference supporting evidence through the Context Graph.

Users should be able to inspect the reasoning chain:

```
Brief Card

↓

Attention Candidate

↓

Context Graph

↓

Evidence

↓

Artifacts
```

---

# Tone

The Executive Engine should maintain a consistent tone:

- Calm
- Professional
- Respectful
- Transparent
- Non-manipulative

It should avoid urgency inflation.

---

# AI Usage

AI may assist with:

- Summarization
- Natural language generation
- Narrative refinement

AI must not fabricate evidence or reasoning.

All generated text must remain traceable to underlying data.

---

# Personalization

Presentation may adapt based on:

- Preferred level of detail
- Reading habits
- Device type
- Accessibility settings

Personalization affects presentation, not truth.

---

# Multi-Modal Output

The same executive product should support:

- Mobile
- Desktop
- Web
- Voice
- Wearables
- Printable reports

Presentation layers should consume a shared product model.

---

# Determinism

Where possible, the Executive Engine should produce stable output given identical inputs.

This improves:

- User trust
- Testing
- Explainability
- Debugging

---

# Failure Handling

If a supporting service fails, the Executive Engine should degrade gracefully.

Examples:

- Omit unavailable sections
- Surface reduced confidence
- Explain missing information

A partial briefing is preferable to no briefing.

---

# Non-Goals

The Executive Engine does **not**:

- Execute external actions
- Modify the Context Graph
- Update memory
- Rank attention candidates
- Trigger notifications

It prepares information for human decision-making.

---

# Related Documents

- SPEC-0005 Attention Engine
- SPEC-0006 Executive Brief
- SPEC-0007 Memory Engine
- SPEC-0004 Context Graph

---

# Guiding Principle

> **The Executive Engine transforms understanding into clarity.**

It does not decide for the user.

It prepares the user to decide well.
