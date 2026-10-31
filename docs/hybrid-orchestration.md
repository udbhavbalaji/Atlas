# Hybrid orchestration migration

Atlas keeps canonical records, validation, authorization, execution, and
persistence in Go. Jev supplies decisions through OpenRouter's Decisions API;
the current API returns choices, not arbitrary JSON arguments or prose. An
OpenRouter chat-completion model currently extracts arguments after Jev selects
a tool. See [PLANNING.md](../PLANNING.md) for the separate preview-only
multi-action experiment.

## First increment: selected tool contracts

`internal/routing/tools.go` is the registry for the tools in the production Jev
category/tool pipeline. Each definition has an ID, category, channel,
read-only marker, description, and JSON argument schema. Category choices,
channel mapping, and extracted-plan matching all use this registry. The
argument planner receives the selected tool's schema when the selected model
supports strict JSON schema output; providers that only support JSON mode
still pass through Atlas's local plan and record-reference validation.

The schemas describe one selected tool and allow empty values for unresolved
fields. They do not grant permission to execute. Existing channel handlers
collect missing fields, and existing capture/mutation paths own writes.
`GET /api/v1/routing/tools` exposes the definitions for inspection. The
`/orchestration-test` webpage shows schemas and can evaluate a route without
dispatching or saving it. Mock evaluation makes no model call.

## Reviewed reminder and note capture

A reminder request with an attached note now stays in one capture. Jev selects
the reminder tool, and the extractor can fill its `note_body` argument. If the
note text is absent, the conversation asks for it instead of silently saving
only the reminder. If Jev selects a different tool, Atlas asks the user to
choose the reminder channel rather than save a partial result. The combined
preview requires one explicit confirmation;
the store commits the reminder, linked note, and idempotency receipt together.
This narrows the prior Jev auto-commit behavior for this compound capture.
Other complete Jev captures retain their current behavior for compatibility.

The conversation section of `/orchestration-test` exercises the same API with
Mock or Jev. Mock asks for any missing fields without model calls. In both
cases, the proposal can be inspected before confirming, cancelling, or
correcting it. The saved result includes the reminder and note link.

## Migration sequence

1. Add a versioned operation proposal and per-operation result envelope around
   the existing conversation lifecycle. Keep existing confirmation receipts.
2. Move argument extraction behind a provider-independent interface with
   configurable model choice and explicit failure behavior. Skip extraction
   when validated arguments already exist.
3. Add bounded multi-operation proposals and execution using existing store
   methods. Preserve atomic linked capture; report independent results
   separately.
4. Add local memory records, provenance, retention decisions, and durable
   background jobs. Memory remains separate from explicit notes.
5. Add disclosure-filtered context retrieval with keyword and metadata search,
   then measure whether local embeddings improve relevant recall. Bound
   automatic and Jev-requested retrieval by rounds and tokens.
6. Consolidate compatible memories, retain conflicting claims, and measure
   latency, model calls, retrieval quality, and action reliability.

## Decisions still required

The current Jev conversation still auto-commits most complete captures, while
older API documentation describes explicit confirmation. Decide which further
operation classes require review and how a user enables automatic execution.
The current Decisions API also cannot write the final response;
Atlas's canonical response remains authoritative, with an optional separate
wording model. Memory retention and model disclosure need their own explicit
policies before automatic memory storage is enabled.

## Manual check

Open `/orchestration-test`, inspect the `reminder` schema, and evaluate the
sample request. Evaluation remains read-only. Then start the conversation with
Mock / Reminder, answer the requested fields, and check that the combined
proposal is shown before saving. Confirm once and inspect the saved reminder
and linked note. Repeat with Jev only when a key is configured and a live
model check is wanted.
