# API-only feature-context rubric

This rubric was fixed before reading the blind agent's report. The target is the cached inferred feature **AI-Enabled Explorer Server**. Each item is worth one point unless stated otherwise. A point requires a materially correct explanation grounded in API-returned code evidence; merely repeating the inferred feature summary does not count.

1. Discovers the repository and latest snapshot, follows the `saved_snapshot` hint, and keeps evidence pinned to the analyzed snapshot.
2. Treats the feature description as inferred and partial rather than proof of runtime behaviour.
3. Identifies `explorerServer` as the feature evidence and traces its callers to application startup.
4. Explains that startup loads configuration, connects/migrates PostgreSQL, starts repository tracking, builds the HTTP server, starts the analysis worker, and supports graceful shutdown.
5. Identifies separate answer and analysis intelligence services/models.
6. Explains that the analysis service may also provide function review capability.
7. Explains that disabling AI indexing prevents the worker from being available even if an analysis service can otherwise be constructed.
8. Identifies the dependencies wired into the Explorer: catalog/knowledge store, tracker, repository scope, bearer token, answer intelligence, reviewer, jobs, Cloud flag, and automatic-feature flag.
9. Identifies protected deterministic read APIs for calls, flow, chain, context, feature list, and feature detail.
10. Identifies on-demand answer, answer-stream, saved-answer lookup, and function-review APIs.
11. Explains that feature generation queues durable work and returns without performing inference in the HTTP request.
12. Explains worker startup/reconciliation, polling, leasing, execution timeouts, bounded global admission, and graceful close.
13. Explains automatic versus manual feature jobs, including stale automatic jobs being superseded when source advances.
14. Explains bounded feature batches and durable continuation/checkpointing rather than one unbounded repository-wide model call.
15. Explains feature inference caching and model-identity/provenance checks.
16. Explains validation of model-generated feature shape, limits, and canonical evidence IDs before publication.
17. Explains that published feature evidence is rebound to canonical snapshot symbols and cannot silently cross repositories, snapshots, or runs.
18. Explains answer targeting and validation: generic, symbol, feature, or flow context; conflicting targets are rejected.
19. Explains that feature answers use bounded canonical evidence and treat the feature summary as untrusted inferred context.
20. Explains answer validation/abstention and the requirement for canonical citations on supported answers.
21. Explains saved-answer reuse/invalidation using repository, snapshot/source context, question, model/policy, and evidence-derived keys.
22. Explains atomic persistence of intelligence results with audit records, citation rows, snapshot locking, and lease fencing.
23. Explains concurrency, timeout, retry/backoff, and partial-progress behaviour without claiming the worker blocks source indexing or HTTP requests.
24. Explains authentication and request security: bearer header, fixed-time comparison, same-origin mutation checks, no credentials in URLs, and bounded inputs/responses.
25. Explains Cloud privacy controls: explicit opt-in, fixed Ollama Cloud origin, disabled redirects/proxies, API key confinement, and that source evidence is sent only when Cloud inference actually runs.
26. Explains telemetry/audit privacy: correlation and safe counts/identifiers are recorded without prompts, source bodies, answer text, or lease tokens.
27. Identifies SSE behaviour for job and answer progress, including bounded streams/heartbeats and persistence before the final answer event.
28. Explicitly states call-graph and feature-analysis limitations, including static resolved edges, missing dynamic dispatch, partial symbol coverage, excluded/unavailable source, and possible overlapping feature descriptions.

Maximum: **28 points**.
