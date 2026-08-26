# Engineering requirements and acceptance gates

Status: required constraints for the implementation. The first server/source/parser slice now exists, with local Makefile checks for formatting, vet, complexity, and tests. CI enforcement and several persistence-specific gates remain future work; see [implementation status](implementation-status.md).

## Complexity at most 10

Interpret complexity as cyclomatic complexity per handwritten function/method. For Go, pin `gocyclo` and fail CI when it reports a value above 10. The proposed check is `gocyclo -over 10` against an explicit set of handwritten Go source directories. Include tests; exclude only identified generated/vendor files. Verify the check itself with a deliberately failing fixture before enabling it.

Prefer guard clauses and small functions that own one decision. A large collection of tiny helpers that obscure a single policy is not an improvement. Equivalent frontend checks should enforce the same ceiling when production UI code is introduced. Reference: [gocyclo](https://github.com/fzipp/gocyclo).

## OpenTelemetry and audit

Every user- or agent-triggered execution receives an execution ID and a trace. REST handlers and MCP tools call the same instrumented application services. Requesting an index, querying protected context, generating features, asking a question, reviewing a feature, and changing configuration are execution boundaries. Child work includes source capture, Git comparison, invalidation, database batches, analysis, and model calls.

Capture a structured event for start, material state change, completion, cancellation, retry, and failure. Attributes include trusted actor type/ID, action, repository, source/target snapshot, job or answer ID, outcome, changed-object counts, and execution/trace correlation. Derive actor identity from authentication, not caller-supplied JSON. Background executions use actor type `system` and link to the originating action where known. A filesystem edit cannot reliably identify its author; record `unknown` rather than pretending the Git commit author is the initiating user.

Propagate context through asynchronous jobs. Preserve the execution ID across retries, and give each attempt its own span. Record errors in spans and structured events; avoid duplicate emission at every layer. Trace identifiers and execution IDs appear in API response metadata. High-cardinality IDs belong in traces/logs, not metric labels.

Use an OTLP exporter and a deployment-configured collector. Export failure must be visible in health/operational metrics, with bounded buffering/retries. Trace sampling must not be the sole audit mechanism: write durable, append-oriented audit records and a telemetry outbox for state-changing operations in the same PostgreSQL transaction as the change. Export outbox events at least once with stable event IDs so downstream consumers can deduplicate. Retain audit records according to an operator policy. These records are not claimed to be tamper-proof against a database administrator.

Protect source and personal information. Exclude raw code, patches, prompts, answer bodies, tokens, credentials, SQL parameter values, and arbitrary request bodies by default. Any opt-in content debugging needs explicit authorization, retention, and redaction. Record source hashes and evidence IDs instead.

Tests use an in-memory OTEL exporter and a real outbox/DB path to verify parent-child linkage, required events, trusted actor assignment, redaction, errors, retries, rollback behavior, exporter outages, and at-least-once deduplication. Reference: [Go instrumentation](https://opentelemetry.io/docs/languages/go/instrumentation/).

## DRY and separation of concerns

Proposed package boundaries:

```text
cmd/denverr/               native command, flags, signals and version metadata
internal/server/            process lifecycle and dependency wiring
internal/transport/http/   HTTP decoding, response encoding
internal/transport/mcp/    MCP protocol adapter
internal/application/      use cases, authorization, transactions
internal/domain/           identities, evidence and snapshot invariants
internal/indexer/golang/   Go parsing, typing, relationships
internal/source/git/       capture, diff, watch and reconciliation
internal/store/postgres/   SQL, persistence and migrations
internal/semantic/         retrieval, feature inference and answers
internal/telemetry/        tracing, audit and exporter integration
```

No SQL in handlers, no model prompts in the parser, no network clients in domain entities, and no UI-specific layout objects in structural analysis records. Enforce transactions at the use-case boundary. Share policies across REST, MCP, jobs, and the UI backend; do not copy business logic between transports. Interfaces belong at actual substitution boundaries, not around every concrete type.

## No N+1 and no application-side database filtering

Authorize and constrain by repository/snapshot in SQL, including nested evidence retrieval. Apply filters, stable ordering, counts, aggregates, and pagination before returning rows to Go. Use explicit columns. Do not `SELECT *` from a large table and discard most rows in application code.

For a file page, aggregate symbol counts in the page query or one bounded batch; do not run a symbol query per file. For a feature detail, load claims and their evidence in bounded set-based queries. For a context request, select ranked candidates and bounded source excerpts in SQL before composing the response. For a call chain, use bounded recursive SQL or set-based frontier batches; never a query per node. Batch size and maximum depth bound the number of round trips. Use bulk/COPY/upsert batches for indexing writes rather than per-symbol existence checks.

Go may parse source bytes, compute types, analyze a bounded source program, serialize already selected rows, and assemble a budgeted response. Those are not excuses to filter previously persisted database data in Go. If model reranking is later required, SQL first returns an explicit, bounded candidate set under a documented retrieval policy; do not silently introduce a broad in-memory filter.

Integration tests instrument query counts and row counts. Compare fixture sizes such as 1, 10, and 100 files/claims and assert the documented query budget does not grow per item. Snapshot authorization tests ensure no excluded rows or source excerpts cross the boundary. Inspect query plans and indexes with representative data, not just empty tables. Add query deadlines and page/fan-out limits.

## Comments explain why

Explain reasons where the code alone cannot: retaining a snapshot after a failed update, distinguishing a candidate call from a resolved call, invalidating reverse dependencies, suppressing unsafe Git helpers, or batching to preserve a query budget. Link to a design decision or regression when helpful. Do not add comments like “increment counter” or use comments as a substitute for good naming.

## Unit and integration tests

Each behavior change includes tests or a documented explanation of why existing coverage is sufficient. Bug fixes add regression coverage at the nearest existing test seam. Do not add tests that only restate constants or configuration.

Unit tests cover small domain decisions, typed extraction, diff parsing, invalidation rules, chain limits, evidence validation, and model failure handling. Integration tests exercise real PostgreSQL migrations/constraints/queries, Git state transitions, job leases and crash recovery, OTEL/audit propagation, HTTP/MCP authorization, native startup, and release-archive creation.

Dependency/version fixtures cover selected versus declared versions, direct/indirect scope, pseudo-versions, multiple modules, `go.work`, local and versioned replacements, vendor mode, missing module caches with network disabled, exact/multiple tags, dirty tagged trees, absent/conflicting version declarations, and tag changes without a file diff. Assert SQL-side pagination and bounded usage-query counts.

Ollama adapter tests use a controllable HTTP fixture for transport, streaming, schema validation, timeouts, cancellation, overload, missing models, model identity, and telemetry redaction. Add separately runnable integration tests against an explicitly provisioned Ollama instance; tests must not download models automatically. Local remains the default. Cloud requires explicit operator opt-in, a server-owned API key and the approved HTTPS provider origin; tests must verify credential confinement and source-transfer disclosure. Model-quality evaluation pins the available model identity and source snapshot, while allowing output variation; a cloud revision is not a reproducible weights digest. Structural indexing must pass with Ollama unavailable, and no test may silently enable a cloud fallback.

Required source-tracking cases include staged and unstaged edits; newly tracked and untracked files; ignored files; deletion; rename/copy ambiguity; repeated dirty edits while HEAD stays unchanged; branch switching; detached HEAD; rebases/force pushes; merge conflicts; unborn repositories; submodule pointer changes; filenames with spaces/newlines; watcher overflow; duplicate/out-of-order notifications; transient parse failures; and source changes during capture.

Proposed CI gates: formatting, static analysis, complexity <=10, unit tests, race checks where supported, PostgreSQL/Git integration tests, API contract validation, security/authorization regressions, and production UI checks once that UI exists. A check is not “passed” until it has actually run in the relevant environment.
