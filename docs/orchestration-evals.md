# Orchestration evaluation suite

The versioned corpus in `internal/httpapi/evaldata/orchestration_v1.json` describes
intended operations and observable outcomes. It is deliberately separate from
the older regression corpus, which mainly asserts current routing states.
Cases are run against a fresh temporary SQLite database. The runner does not
read or write the app's database.

## Run the offline suite

```sh
GOCACHE=/tmp/atlas-go-build go run ./cmd/atlas-eval \
  -mode offline -revision development -out /tmp/atlas-development-eval.json
```

The offline suite injects scripted Jev choices and extractor plans. It measures
action conservation, review state, linked writes, cancellation, and call count
**assuming the supplied model choices and arguments**. It does not measure
whether a real model understood the request. A target gap is reported in JSON
and does not fail the command unless `-strict` is set. On `development` at
`53f98ac`, six of twelve initial cases pass. Five reminder-with-note cases
describe PR 18 behavior not yet present there. On PR 18 at `7c3d528`, eleven
of twelve pass. The remaining case shows a reminder with an attached note
followed by a separate deletion being split into three steps instead of two.

Open `/orchestration-eval-test` on the local Atlas server to run these cases,
inspect each check, and download a report. The page can also import a JSON
report from a CLI live run. The webpage only runs the offline mode itself.

## Run small live probes explicitly

These commands require configured credentials. No live mode is part of
`go test ./...`, and none is called by the webpage. Every live mode has a hard
request cap, one configured model per provider, and no automatic retry.

```sh
# Jev category/tool decisions through OpenRouter: at most four requests.
GOCACHE=/tmp/atlas-go-build go run ./cmd/atlas-eval \
  -mode jev -allow-live -out /tmp/atlas-jev-eval.json

# Groq argument extraction: two requests, no OpenRouter dependency.
GOCACHE=/tmp/atlas-go-build go run ./cmd/atlas-eval \
  -mode groq -allow-live -out /tmp/atlas-groq-eval.json

# Current conversation pipeline with Jev decisions and Groq extraction.
GOCACHE=/tmp/atlas-go-build go run ./cmd/atlas-eval \
  -mode mixed -allow-live -out /tmp/atlas-mixed-eval.json
```

`-max-requests N` can lower or raise a live cap, up to 20. The Jev probe uses
Atlas's real routing path with extraction held unavailable, so local guards
and the actual category/tool questions are measured without a second model.
The Groq probe
checks argument extraction under a supplied selected tool. The mixed probe
uses the conversation endpoint and a fresh store. Its Jev+Groq combination is
an evaluation configuration; the normal app currently uses OpenRouter free
models for Jev's extraction stage. Reports label the mode and model names.
Live semantic samples are intentionally small; treat their failures as cases
to inspect, not a statistically stable accuracy estimate.

## Compare revisions

Run the same mode and corpus on two checkouts. The comparison pairs case IDs
and reports improved, regressed, added, and removed cases separately.

```sh
GOCACHE=/tmp/atlas-go-build go run ./cmd/atlas-eval \
  -mode offline -revision pr18 -baseline /tmp/atlas-development-eval.json \
  -out /tmp/atlas-pr18-eval.json \
  -comparison-out /tmp/atlas-pr18-comparison.json
```

The runner is isolated in new files so it can be brought onto PR 18's branch
before that PR merges. After PR 18 is integrated with the latest `development`,
run the paired suite again. Existing cases should keep their IDs; change the
corpus version when their meaning changes.

## What the report means

Each case shows its intended operation, observed conversation checkpoints,
record counts, linked-note count, scripted or actual provider requests, token
usage when returned, and elapsed time. Slice totals and p50/p95 elapsed times
are included. Offline times are useful for regression checks on the same
machine; live times also include network variability. A correct selected tool
does not by itself mean the final operation was correct, so linked writes and
review transitions are checked separately.

Next corpus expansions should cover additional phrasing, exact target IDs,
ordered dependent operations, stale reviews, read-after-write, failure
injection, context recall, and final-response factuality as the versioned
operation proposal and retrieval layers land. Preserve the outcome labels and
adapt the observation code to the new response envelope.
