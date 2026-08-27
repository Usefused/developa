# Implemented explorer API

This is the served API. The browser consumes these same endpoints. The [generated OpenAPI 3.1 contract](../api/openapi.json) describes their schemas and limits and is available from the engine at `GET /api/openapi.json`. See [generation and validation](../api/README.md).

## Authentication and scope

Configure `REPOSITORIES` (or `REPOSITORY_PATH`) to seed checkouts, or `WORKSPACE_ROOTS` to add them through the UI/API. `denverr serve` creates a private operator token on first run unless `DENVERR_API_TOKEN` supplies one of at least 24 bytes. Supply `Authorization: Bearer <token>` on all protected requests. Saved registrations restore from PostgreSQL on startup. Registration accepts a relative folder within an operator-allowed root, never arbitrary absolute paths, model configuration or actor identities. The shared operator token grants access to all registered repositories; it is not per-repository user authorization. See [workspace registration](repositories.md#filesystem-browsing-api).

For multiple repositories, resolve a known engine-visible root with `POST /api/repositories/resolve`, or call `GET /api/repositories?q=&limit=24&offset=0`, then prefix every project operation with `/api/repositories/{repositoryID}`. For example, `/api/repositories/{repositoryID}/project` and `/api/repositories/{repositoryID}/snapshots/{snapshot}/flow`. All routes below under `/api/project`, `/api/scan`, `/api/capabilities` and `/api/snapshots/...` also exist at that repository prefix, including SSE and function reviews. The shorter routes remain aliases for the **first configured repository**. Agent clients should use explicit repository IDs; details and setup are in [repositories.md](repositories.md).

UI pages/assets, `/healthz`, `/readyz`, `/api/info`, and `/api/openapi.json` are public. Info returns `configured`, `authentication_required`, and an optional `workspace_management` boolean. An empty engine with allowed folders still requires authentication before adding a workspace. No CORS access is enabled. Scan and registration requests reject a foreign Origin or cross-site browser request; forwarded proxy headers are not trusted.

## Routes

| Method | Path | Response |
| --- | --- | --- |
| GET | `/api/info` | Public setup/authentication flags |
| GET | `/api/workspace-roots` | Authenticated allowed filesystem locations |
| GET | `/api/workspace-folders?root_id=ID&path=.&offset=0` | Bounded folder page within an allowed location |
| POST | `/api/repositories` | Persist and monitor a Git workspace; HTTP 201 or 200 for an existing registration |
| POST | `/api/repositories/resolve` | Resolve an exact engine-visible repository root to its ID and latest snapshot |
| GET | `/api/repositories` | Paginated registered workspaces and latest snapshots |
| GET | `/api/project` | Repository ID/name, tracker status, watching flag, polling interval, safe last error, latest snapshot metadata |
| GET | `/api/capabilities` | Authenticated capability flags, including whether Ollama is configured (not a live readiness check) |
| POST | `/api/scan` | HTTP 202 with execution ID, authenticated operator actor, trigger, trace ID, queued status |
| GET | `/api/snapshots/{snapshot}/files` | Paginated file summaries and declaration-kind counts |
| GET | `/api/snapshots/{snapshot}/file?path=internal/example.go` | File summary, documentation, imports |
| GET | `/api/snapshots/{snapshot}/symbols` | Paginated `{path, symbol}` records |
| GET | `/api/snapshots/{snapshot}/symbols/{symbol}` | One `{path, symbol}` record |
| GET | `/api/snapshots/{snapshot}/symbols/{symbol}/source` | UTF-8-safe chunks of the full retained declaration body |
| GET | `/api/snapshots/{snapshot}/symbols/{symbol}/implementations` | Paged interface implementation candidates with source references and analysis limits |
| GET | `/api/snapshots/{snapshot}/details` | Snapshot metadata, limitations, diagnostics, exclusions, changes, skipped paths |
| GET | `/api/snapshots/{snapshot}/calls` | Paginated direct call sites, including resolution status and reason |
| GET | `/api/snapshots/{snapshot}/symbols/{symbol}/chain` | Bounded resolved call traversal with nodes, edges and truncation flag |
| GET | `/api/snapshots/{snapshot}/flow` | Bounded application/symbol/feature flow, roots, dependencies, cycles and source evidence |
| GET | `/api/snapshots/{snapshot}/context` | Ranked, bounded source context for an agent or question |
| GET | `/api/snapshots/{snapshot}/features` | Latest feature-generation metadata and paginated inferred features |
| GET | `/api/snapshots/{snapshot}/features/{feature}` | Feature description and canonical source citations |
| POST | `/api/snapshots/{snapshot}/features/generate` | HTTP 202 durable job admission; inference runs independently in the background |
| GET | `/api/snapshots/{snapshot}/analysis-job` | Saved job status, coverage, pages, retries and safe failure code |
| GET | `/api/snapshots/{snapshot}/events` | SSE analysis-job updates from persisted state |
| POST | `/api/snapshots/{snapshot}/answers` | Answer a code question synchronously with validated citations and persistence |
| POST | `/api/snapshots/{snapshot}/answers/stream` | SSE progress and validated, persisted answer; same request schema |
| POST | `/api/snapshots/{snapshot}/answers/lookup` | Read-only lookup of an existing answer for the same question, target and unchanged source context; returns `{answer: null}` on a cache miss |

Source reads include an explicit publication snapshot ID. The repository list, project status and capability flags are not snapshot-pinned. Snapshot and symbol IDs are 64 lowercase hexadecimal characters. Symbol IDs are logical source identities; publication IDs differ from source fingerprints. A fresh execution publishing previously seen content receives a distinct publication ID; retrying the same execution reuses its immutable data.

## Lists and filters

Files and symbols accept `q`, `kind`, `file`, `limit`, and `offset` query parameters. `q` is a literal, case-insensitive substring of file metadata or symbol names/signatures, not a regex or SQL pattern. `kind` is one parser kind such as `function`, `method`, `struct`, `interface`, `field`, or `closure`. `file` selects one repository-relative path. File detail uses `path` instead.

Defaults are 24 files or 50 symbols per page. Limits range from 1 through 100; offsets from 0 through 100,000; query text is limited to 200 bytes. Invalid filters return 400. Responses have `items`, `total`, `limit`, and `offset`; an offset past the end has an empty `items` array but retains the total. Each page is selected, counted, and assembled in one PostgreSQL statement under repository/snapshot scope.

## Manual indexing

Send an empty request or `{}` to `/api/scan`; use `Content-Type: application/json` if sending a body. Unknown fields and bodies larger than 1 KiB are rejected. HTTP 202 acknowledges a durably audited admission to the current process's queue, not completion or restart-safe job replay. Poll `/api/project` for status and the latest publication. Concurrent queued/running work returns 409.

Request context propagates into the background trace while its cancellation is separated from the HTTP connection. Server shutdown and the scan deadline still cancel work. Startup/watch activity is attributed to the system; a shared-token caller is attributed to the operator. These are not individual authenticated user identities.

## Evidence and errors

Every symbol includes a signature, kind, documentation where present, source identity/hash, and physical positions. Lines/columns are one-based UTF-8 byte positions and ends are exclusive. Type text is parsed syntax, not a resolved type or a verified runtime response schema.

Symbols include a declaration `source` excerpt of at most 8 KiB and `source_truncated` when applicable. Snapshot details include independent `call_analysis` status, resolved/unresolved counts, limitations and diagnostics. `index_version` identifies the extraction version; older indexes are reconciled automatically once after upgrade.

`source_complete` is completeness within capture policy; `completeness` is syntax extraction completeness. `changes_known: false` means the capture lacked a previous in-process manifest, not that zero files changed. Initial/restart captures cannot reconstruct every missed filesystem edit.

Responses include `X-Trace-ID`. Error bodies expose a stable `status`, never driver errors, SQL, source text, or credentials: 400 invalid input, 401 invalid authentication, 403 rejected origin, 404 absent scoped records, 409 busy/stale continuation, 502 invalid model output, 503 unavailable/unconfigured repository/model, and 504 execution deadline. Failed scans or rejected AI output retain prior publications.

## Calls and traversal

`/calls` accepts optional `symbol_id`, `direction=in|out` (default out), `resolution=resolved|unresolved|external|builtin`, and bounded `limit`/`offset` (default 50/0, max 100/100000). Without a symbol ID it lists call sites in the snapshot. Resolution is a static binding classification, not evidence of execution.

`/symbols/{symbol}/chain` accepts `direction=in|out`, `depth=1..5` (default 2), and `limit=1..100` (default 40). Only resolved edges are traversed. A SQL breadth-first search caps nodes/edges, avoids repeated paths/cycles, and prioritizes discovery edges so each returned node stays connected. `truncated` covers node, edge and depth limits. Zero incoming resolved edges does not prove a function is unused.

## Reusable context and answers

### Dependency navigation and complete bodies

Read the [agent traversal guide](agent-context.md) for a generic evidence-gathering procedure. Reuse a supplied/cached API contract rather than repeatedly downloading OpenAPI.

New call records include a `target` reference for resolved local declarations. Unresolved interface calls can include `interface` and `interface_method` references. References contain `symbol_id`, name, file path and physical span. The call's own path/span still identify the call site. Follow `/symbols/{symbol}/implementations` using the interface or interface-method ID, then fetch each relevant `target.symbol_id` through the symbol endpoint. Candidate pages use `limit=20` by default (maximum 100), `offset=0..100000`, and retain `total` plus analysis limitations on empty pages. Selection, counting and pagination use one SQL statement.

Candidates are separate from resolved call edges: `go_types_method_set` indicates a static method-set relationship, while `signature_match_with_unavailable_types` is a conditional signature match without complete imported type evidence. Neither proves a runtime receiver. Generic instantiations, external embeddings, excluded build/test variants and runtime wiring remain limited; read the returned analysis status. Existing chain/flow traversal continues to use resolved edges only. Pre-upgrade snapshots report implementation analysis unavailable rather than claiming no implementations exist.

Symbol previews remain bounded at 8 KiB. `/symbols/{symbol}/source?offset=0&limit=8192` retrieves retained source in chunks of 4..16384 bytes. The offset is relative to the declaration, in UTF-8 bytes; follow `next_offset` exactly until null. The response preserves the full definition span, source identity/hash and total byte length. `complete` means all declaration bytes were retained, not that this chunk is the last. The database selects the bounded slice; neither this endpoint nor historical reads consult the current checkout.

Index version 4 stores captured Go file bytes once per file/publication, outside the ordinary JSON file/symbol payloads. This increases snapshot storage; existing capture exclusions and API authentication still apply. Older complete previews can satisfy source reads with an explicit limitation; missing/truncated historical source returns `409 source_unavailable`. Reindexing creates a new publication and does not modify old evidence. These deterministic retrieval operations never invoke Ollama.

The [flow API guide](flow-api.md) describes agent exploration through small scoped calls, upward ancestry plus seed descendants, API-provided dependency/cycle facts, and explicit flow explanations. No browser or model is needed for flow retrieval.

`/context?q=...&limit=12` accepts up to 2000 UTF-8 bytes of query text and 1..20 results. PostgreSQL ranks literal word tokens over names, signatures, comments and stored source; this is full-text retrieval, not vector/semantic search. Empty text selects a deterministic bounded page. The response carries repository/snapshot IDs, source records, total matches and a truncation flag.

Send `{"question":"What does this do?","symbol_id":"optional-64-hex-id"}` to `/answers`. Alternatively supply `feature_id` to explain a saved feature using its bounded canonical evidence, or `flow` with the flow endpoint's selection/depth/limit options. The three targets are mutually exclusive. Questions require 1..2000 decoded UTF-8 bytes; the raw JSON body is bounded to 16 KiB to allow escaped Unicode. Unknown fields, caller-selected models/URLs/actors and multiple JSON objects are rejected. Supplying a symbol prioritizes that exact snapshot record and a bounded neighborhood of resolved callers/callees; without a target the question drives bounded SQL retrieval.

The response has an answer ID, snapshot ID, pinned model identity, text, canonical citations, `cached`, `insufficient_evidence`, `context_truncated`, `limitations`, and creation time. Source and inferred feature claims are untrusted prompt data, never executable instructions or independent proof. Outputs must satisfy the schema and cite only supplied IDs. Matching questions/evidence/model/prompt reuse validated cached inference with current source positions. Abstention uses a fixed insufficient-evidence response. Successful answers are persisted with audit/outbox rows.

`POST /answers/lookup` accepts the same request schema but **never invokes Ollama**, including model metadata checks. It uses a request body so questions do not appear in URL/access logs. It works when inference is disabled or the model is offline, returning the latest saved document with matching source/context and prompt rules. There is no time-based expiry. The source lookup is bounded and snapshot-scoped; the final answer/citation lookup uses one indexed SQL statement. Function content hashes include changes beyond truncated excerpts. Changes to the function, supplied supporting implementations, topology or selected feature evidence invalidate reuse; unrelated edits and shifted line numbers do not. A missing target remains 404; valid targets without a matching saved answer return `{answer: null}`. No miss starts generation.

Saved reads preserve the original answer ID, model, text and creation time, add `generated_snapshot_id`, and rebind canonical citations to the requested `snapshot_id`. They do not publish a new answer or alter historical snapshots. The UI restores this content on reopening and disables repeat generation while it has a saved explanation. There is no general answer-history listing endpoint.

Migration 008 adds the request/context fingerprint. Older answer rows did not record their question or target, so they cannot safely be assigned to a particular explanation. One explicit request records the new identity; existing validated inference-cache entries can still avoid a new model call when their evidence/model/prompt match.

`/api/capabilities` includes `ollama_configured`, `ollama_cloud`, `answers`, `analysis_jobs` and `automatic_features`, configuration flags rather than live model readiness. Local inference is the default. Explicit `OLLAMA_CLOUD=true`, an operator-owned `OLLAMA_API_KEY`, and the HTTPS `ollama.com` origin enable external inference. `OLLAMA_BASE_URL` aliases `OLLAMA_URL`; `/v1/` and `/api/` normalize to the origin for native protocol requests. Conflicting endpoint settings are rejected. Source excerpts and questions are then sent to Ollama. Credentials are not accepted in request bodies, included in API responses, persisted in source records or exported in telemetry. There is no automatic cloud fallback.

Local identity is `model@sha256:weights-digest`; cloud identity is `model@cloud:provider-revision`. A cloud revision is not a reproducible weights digest. Cloud does not enforce structured output schemas: the adapter requests JSON through the prompt, then application validation rejects malformed results and invented evidence. Both modes disable proxies and redirects. The browser discloses cloud source transfer before generation.

## Durable feature indexing

### SSE result delivery

The authenticated job-event stream emits `analysis` with the current job DTO, including initial `not_queued` state. Updates are deduplicated while connected. Keepalive comments keep idle connections active; the bounded stream lifetime requires reconnection, which resynchronizes current database state rather than replaying historical events. The browser uses authenticated fetch streams so secrets never enter query strings. Job events perform only bounded stored reads and never trigger inference.

Routine project refresh and idle job-state reads occur every two minutes; queued/running job streams check progress every second. Reopening the view or explicitly queueing work reads current state immediately. These UI delivery intervals do not change the engine's `WATCH_INTERVAL` or worker cadence. Each SSE frame is bounded to 256 KiB. Canonical citation metadata must encode within 8 KiB per citation; oversized names/paths are rejected before inference rather than silently changing source identities.

`/answers/stream` accepts the same request as `/answers`. It emits `started`, then `answer` only after application validation and `SaveAnswer` succeed, or a safe numeric-status `error` event after streaming begins. Preflight failures remain ordinary HTTP JSON errors. The browser does not automatically retry an inference POST. Cancellation closes the request context, while previously admitted background jobs remain independent. SSE is downstream from persistence; partial provider output is neither shown as saved documentation nor written to the index. Raw token streaming is not implemented.

### Queue and cost controls

Send an empty body or `{}` to `/features/generate`. It returns HTTP 202 with a durable PostgreSQL job, using the ordinary HTTP request timeout; it does not wait for inference. Duplicate requests while a job is queued/running return that active job. Failed jobs can be retried; a completed job can be explicitly rebuilt. A client disconnect after admission does not cancel work. This endpoint requires an enabled worker (`AI_INDEX_ENABLED=true`) and a configured model.

Discovery is explicit by default. Optional `AI_AUTO_FEATURES=true` admits automatic analysis only for clean, complete snapshots with a Git commit. A durable per-repository commit ledger prevents duplicate automatic jobs, including after restart or manual promotion. Dirty edits still update the structural index without invoking a model; manual generation may explicitly analyze them. Turning automatic discovery off pauses existing automatic jobs, not manual requests. Polling can miss intermediate commits or a clean state followed immediately by more edits; this is not a Git commit-object backfill service.

The worker uses `FOR UPDATE SKIP LOCKED`, a lease of `AI_TIMEOUT + 30s`, bounded retries and safe failure codes. `AI_TIMEOUT` defaults to 120s (maximum 5m); `OLLAMA_TIMEOUT` bounds each model request (default 60s). Polling defaults to `AI_POLL_INTERVAL=2s` (maximum 1m). No NATS, Redis or in-memory queue is required for analysis jobs. One server can monitor multiple checkouts. Background workers share one execution slot per server, acquired before claiming a lease, so adding repositories does not multiply simultaneous background model calls. This is an admission guard, not a second job queue; jobs/checkpoints remain in PostgreSQL. Interactive, explicitly requested explanations retain their own execution gates.

Set `OLLAMA_FEATURE_MODEL` for background Features, `OLLAMA_REVIEW_MODEL` for saved function-card metadata, and `OLLAMA_ANSWER_MODEL` for interactive answers. Feature and review roles fall back through the legacy `OLLAMA_ANALYSIS_MODEL`, then `OLLAMA_MODEL`; answers fall back directly to `OLLAMA_MODEL`. Models are operator configuration, never selected by repository content or request bodies.

Each worker chunk processes one file-bounded page, up to 8 symbol records and a 16 KiB evidence budget, then persists a checkpoint and schedules the next chunk. Cached inference is reused when the repository, source facts, verified model revision, prompt/schema and inference policy match. File boundaries prevent unrelated files from shifting page membership. Cache hits are validated again and bound to current source positions; they perform no new chat inference, although model metadata may be checked. Changed pages require inference; changes within large files can invalidate later pages in that file.

`parent_run_id` identifies the prior immutable generation; previous features/citations are copied in SQL while new results are added atomically. Provider and model revision must match for continuation. Expired workers cannot publish, and recovery checks the persisted feature cursor before invoking the model again. Saved coverage survives process restarts. A newer commit supersedes obsolete automatic jobs; dirty edits at the same commit do not. Manual historical requests remain explicit work.

`/analysis-job` returns `not_queued`, `queued`, `running`, `completed`, `failed`, or `superseded`, with the source `commit`, saved page count (`chunks`), symbol coverage, feature count, timestamps, and consecutive failure attempts. It does not expose lease tokens or trace propagation headers. A missing job on a valid snapshot returns `not_queued`; a missing snapshot returns 404. Disable `AI_INDEX_ENABLED` and restart to pause background execution while keeping stored results readable. Feature-run metadata includes cumulative `cached_batches` and `model_calls`; these describe published progress, not provider billing or tokens consumed by failed attempts.

Counts describe examined symbol records, not unique source bytes or proven functionality. A completed job can still have a partial feature run because source excerpts were truncated; every feature remains `status=inferred`. Invalid model output, stale parents, lost leases and failed persistence do not replace prior saved features. Cross-batch synthesis/deduplication is not implemented. Question answering remains a separate synchronous API execution.

`/features` accepts `q`, `limit`, and `offset` only; selection and pagination happen in SQL. `/features/{feature}/context` composes the inferred claim, canonical source declarations, and bounded resolved flow in one read-only agent call. It performs three set-based SQL reads regardless of feature size and never invokes Ollama. Feature IDs are 64 lowercase hexadecimal characters and remain readable in the latest continuation. Each citation's name/path/span comes from indexed symbols rather than model-provided coordinates. The browser renders the same context and opens free-form, target-scoped Ask AI chats only after a user action. Browsing alone never generates an answer.

The UI records `?snapshot=ID` after a successful queue admission, keeping that analysis visible after reload even if the working tree has advanced. Snapshot links are authenticated and scoped through the same APIs. **Show latest** removes the pin; it does not generate another job.


## Source summaries

Function, method, and closure records include `symbol.documentation`:

```json
{
  "summary": "Send delivers a value.\n\nReject empty input before delivery.",
  "origin": "indexed_source",
  "truncated": false,
  "comments": [
    {"kind":"doc","text":"Send delivers a value.","span":{"start":{"line":2,"column":1,"offset":15},"end":{"line":2,"column":25,"offset":39}}},
    {"kind":"body","text":"Reject empty input before delivery.","span":{"start":{"line":4,"column":2,"offset":67},"end":{"line":4,"column":40,"offset":105}}}
  ]
}
```

This is deterministic compilation of comment prose in source order, separated by blank lines; it is not a paraphrase or proof of behavior. Go comment markers/directives are stripped by the Go AST comment reader. Strings are not mistaken for comments. Nested closures own their body comments separately. The original declaration `doc` / `comment` fields remain available. Empty prose stays empty; no behavior is inferred from a function name.

Since index version 3, comments are extracted from the full captured file independently of the 8 KiB implementation excerpt, with at most 64 comment groups and 8 KiB compiled text. Truncated prose or partial parsing is flagged. Spans use physical snapshot coordinates. Historical records without this field are enriched during bounded reads using only saved docs and source (`origin: captured_excerpt`), without extra SQL queries, working-tree access, or snapshot mutation. Legacy declaration docs may have no span because their original comment coordinates were not saved.

The same API object is available through symbol, page, chain, flow, context, and review reads. Cards and sidebars render this source summary by default. AI reviews remain optional and separate; reading summaries never invokes Ollama.

### Explanation context

All AI paths share captured signatures, implementation excerpts, compiled comments, and explicit truncation flags. Signatures preserve parameter/return types; comments may describe their purpose but are not assumed correct. Source hashes also invalidate cached inference when changes fall beyond an excerpt. Short reviews additionally send structured parameter positions and remain independent within a batch.

A `symbol_id` answer uses one symbol lookup and one bounded graph query in the PostgreSQL implementation: the selected declaration first, then direct resolved callers/callees (depth 1, at most 9 nodes total). The model receives supporting implementations, directed relationships, counts of unresolved/external sites, and an explicit focus ID. No per-neighbor queries or extra model calls are made. Feature explanations use their canonical evidence; flow explanations use their selected graph. The shared 16 KiB evidence budget can omit neighbors or shorten excerpts, and responses/UI disclose incomplete context. This is not the whole repository: external implementations, transitive dependencies, runtime dispatch, and arbitrary referenced type definitions are not fetched automatically. Unknown behavior must remain unknown.
