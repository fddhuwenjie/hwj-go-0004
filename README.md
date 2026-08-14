# flagship

`flagship` is a standard-library-only Go feature-flag evaluation service. It provides a concurrency-safe in-memory library and a JSON batch command that can create flags, import configurations atomically, evaluate targeting rules and stable rollouts, resolve prerequisites, enforce timezone-aware schedules, and query evaluation audit history.

## Public behavior

- Lower numeric rule priorities are evaluated first; every condition in a rule must match.
- Percentage rollout weights use basis points and must total 10,000. A subject receives a stable result for each flag.
- Prerequisites are evaluated recursively. Missing flags and cycles return typed errors.
- A schedule uses local wall-clock timestamps with an IANA timezone and a half-open start/end interval.
- Batch imports validate every definition before changing state. Optional request IDs make retries idempotent.
- Returned flags and audit slices are isolated copies of service state.

## Verify

```sh
GOTOOLCHAIN=local go test ./...
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go build ./...
```

## Command

Run a JSON object or array through the batch CLI:

```sh
go run ./cmd/flagship -in examples/workflow.json -out examples/result.json
```

Supported operations are `upsert`, `import`, `evaluate`, and `audit`. The command creates one fresh in-memory service per batch, so state created by earlier commands is available to later commands in the same input.
