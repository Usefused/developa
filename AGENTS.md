# Engineering requirements

These requirements apply to product implementation and changes in this repository. See `docs/engineering-requirements.md` for enforcement and acceptance criteria.

- Maximum cyclomatic complexity: 10 per handwritten function/method, including tests. Decompose by responsibility; do not hide complexity in an oversized dispatcher. Pin the complexity checker. Generated and vendored code must be clearly identified, not hand-edited or used to conceal logic.
- Instrument user- and agent-triggered executions with OpenTelemetry, propagating execution/trace identity through jobs, database work, analysis, and model requests. Emit change, completion, and failure events. Never export source, secrets, credentials, or prompts by default. Preserve a durable audit record separately from sampled traces.
- Follow DRY: share real domain logic and invariants; avoid speculative abstraction.
- No N+1 database access. Use bounded set-based queries, joins, aggregations, and batched writes. Test query counts against small and larger fixture sets.
- Filter, authorize, sort, aggregate, and paginate stored data in SQL. Do not fetch a broad dataset and filter it in Go. Parsing new source and computing analysis results are separate from database retrieval.
- Separate transports, application services, domain policy, analysis, persistence, source tracking, and model adapters. REST and MCP share services and authorization.
- Comment non-obvious decision logic to explain why it exists. Do not narrate obvious statements or leave speculative comments that disagree with the code.
- Add or update unit and integration tests for changed behavior. Exercise real PostgreSQL and temporary Git repositories where relevant; mocks alone do not verify persistence, query counts, or change tracking.
- Keep reads pinned to source snapshots. Distinguish resolved relationships, candidate targets, and inferred feature claims. Never present incomplete analysis as complete.
- Use Ollama for AI; keep local inference as the default and never silently fall back to cloud models. Treat model configuration as operator-owned, not repository-controlled.
- Extract dependency and version metadata with Git/Go tooling, not AI. Keep selected dependency versions, project release declarations/tags, Git revisions, and Go toolchain versions distinct. Missing version evidence remains unknown.
- Run appropriate checks before claiming completion. Clearly disclose unexecuted checks and unimplemented behavior.
