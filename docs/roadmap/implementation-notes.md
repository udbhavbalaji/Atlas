# Implementation Notes

Atlas implementation work begins with the Go Event Kernel.

The first kernel slice provides:

- canonical event validation
- a static event catalog registry
- append-only JSONL event persistence
- deterministic replay
- a small local CLI

Run a local publish smoke test with:

```sh
go run ./cmd/atlas events publish \
  --type ArtifactCaptured \
  --source capture.manual \
  --actor User \
  --sensitivity Personal \
  --payload-json '{"note":"hello"}'
```

Replay local events with:

```sh
go run ./cmd/atlas events replay
```

Runtime event data is written under `.atlas/`, which is local-only and ignored by Git.

