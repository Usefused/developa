# Feature-context API benchmark — round 1

## Question

Can a fresh coding agent explain the cached inferred feature **AI-Enabled Explorer Server** using only the Developa HTTP API, without reading the checkout, Git, PostgreSQL, local documentation, or another agent's notes?

The question asked for initialization, service wiring, HTTP capabilities, automatic versus request-time work, feature generation, answer persistence, reliability, audit, privacy, and security boundaries. The agent also had to cite API-returned paths, symbols, IDs, and line spans; separate facts from inference; and list unknowns.

The [rubric](rubric.md) was fixed before the blind report was read.

## Result

**25.5 / 28 points (91.1%).**

The API provided enough information for a strong implementation-level explanation. The agent did substantially more than repeat the inferred feature summary:

- It discovered the current repository and snapshot, noticed the `saved_snapshot` pointer, and moved to the older snapshot that actually owns the feature evidence.
- It traced the single feature citation, `explorerServer`, upward to startup and downward through HTTP, intelligence, worker, model, and persistence helpers.
- It recovered the dual answer/analysis model design, AI-index disable switch, automatic-feature opt-in, deterministic read APIs, request-only model operations, durable feature queue, leases, resumable bounded batches, model/evidence validation, transactional publication, audit records, answer citation validation, authentication, same-origin checks, cache policy, and Cloud endpoint restrictions.
- It correctly distinguished an already-published partial feature run from the durable job that later failed after three attempts with `invalid_model_output`.
- It kept the inferred feature summary separate from source-proven facts and explicitly called out the 129/2,612-symbol coverage limit and static-call-graph limits.

## Partial or missed context

- The saved snapshot predates the current saved-answer lookup route. The agent correctly refused to project that route backward, but therefore did not fully reconstruct current saved-answer lookup and invalidation behaviour.
- It found worker polling, leases and execution timeouts, but did not fully explain global admission, graceful worker close, retry backoff, or the nonblocking relationship to source indexing and HTTP serving.
- It identified job and answer SSE endpoints and established that streamed answers use the persisted answer service, but omitted heartbeat, lifetime, write-bound, reconnection, and job-event polling details.
- Cloud security coverage omitted some exact transport controls, particularly disabled redirects/proxies and complete API-key confinement. Audit privacy coverage did not explicitly enumerate answer text and lease tokens among excluded fields.
- One `discoverBatches` citation contains a 96-character concatenated symbol ID. The path, function and line span are correct; this is an agent report transcription error rather than missing API metadata.

## Cost and navigation quality

- 78 HTTP attempts: 77 HTTP 200 responses and one initial sandbox-denied localhost attempt.
- 1,608,632 raw response bytes.
- 853.735 ms summed HTTP response time.
- OpenAPI was attempted three times: one failed sandbox attempt and two successful downloads.
- No Ollama or inference endpoint was called.

This establishes that the information is available, but not yet as a compact feature-context operation. Most overhead came from discovering supporting helpers after the feature record supplied only one evidence symbol. An MCP tool should orchestrate the existing small endpoints while preserving snapshot pinning, candidate-versus-resolved distinctions, pagination and source bounds. A useful `feature_context` tool would return or page through:

1. feature/run metadata and the authoritative evidence snapshot;
2. evidence symbols and complete bodies;
3. feature flow and unresolved interface references;
4. supporting callers, callees and implementation candidates;
5. relevant configuration, persistence and transport boundaries;
6. explicit completeness and inference limitations.

It should reuse a cached contract instead of downloading the full OpenAPI document twice.

## Artifacts

- [Blind agent report](agent-report.md)
- [Complete request ledger](requests.json)
- [Precommitted rubric](rubric.md)
- [Machine-readable score](result.json)
