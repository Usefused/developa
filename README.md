# Developa

Developa is a self-hosted code intelligence project, starting with Go. The intended product is an API with an optional visual explorer and an MCP interface for agents. Developa is the workspace name, not a final product name.

**Status: the explorer is connected to a working index.** One Go server watches multiple configured local checkouts, parses Go declarations, publishes immutable PostgreSQL snapshots, and serves a token-protected API and embedded browser UI. The CLI remains available for standalone scans.

The frontend uses Remix's React Router framework mode, with separate sidebar routes and reusable React components. See [frontend architecture and development](docs/frontend.md). Production still needs no separate Node server.

**Static local call chains, context retrieval, background feature discovery, and codebase answers are implemented.** AI uses operator-selected Ollama models: local by default, or opt-in Ollama Cloud, with separate analysis/answer roles. Working-tree indexing makes no model calls. Durable jobs analyze clean committed snapshots in the background and reuse cached unchanged pages. Features and answers retain snapshot/model provenance and validated source citations. Dynamic targets and incomplete analysis remain visible. MCP and dependency/version inventory are not implemented yet. Git commit, branch, and tags pointing at HEAD are revision evidence, not a guessed project version.

## Run the server

With Docker Compose:

```sh
cp .env.example .env
# Change both password occurrences in .env before using shared infrastructure.
docker compose up --build
```

To use the public multi-architecture image instead of building locally:

```sh
docker pull ghcr.io/usefused/developa:latest
docker compose up --no-build
```

The image is published from [Usefused/developa](https://github.com/Usefused/developa) and supports Linux AMD64 and ARM64. Set `DEVELOPA_IMAGE` to a pinned digest for reproducible deployments.

Open **http://127.0.0.1:8080/** for the explorer. With no repository configured, it shows setup instructions. PostgreSQL has a persistent volume and no published host port. There is no Redis service or separate frontend runtime.

To connect a checkout, set `REPOSITORY_HOST_PATH` to its absolute root in `.env`, set a random `DEVELOPA_API_TOKEN` of at least 24 characters, then use the read-only mount override:

```sh
docker compose -f compose.yaml -f compose.repository.yaml up --build
```

The override mounts that checkout at `/repository` inside the server. It refuses to create a missing host directory. The runtime includes Git and trusts only that explicitly selected repository root, allowing a nonroot container to inspect a checkout owned by another UID. Use the token in `.env` to unlock the UI. A verified token is saved in browser `localStorage`, restored on refresh, and forgotten on **Lock workspace** or a 401 response. Avoid signing in on shared computers; scripts running on this origin can access localStorage.

```sh
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
```

`/healthz` checks process liveness. `/readyz` checks PostgreSQL with a bounded timeout and returns HTTP 503 if it is unavailable. Startup fails if the database, migrations, or configured repository cannot be initialized. Readiness is database availability; indexing status is reported separately by `/api/project`.

For native development, install Go 1.26+, Node.js 22+, and Git, and supply an existing PostgreSQL database. Node is only needed to build/check embedded browser assets:

```sh
npm ci
make build
export DATABASE_URL='postgres://developa:password@127.0.0.1:5432/developa?sslmode=disable'
export REPOSITORY_PATH='/absolute/path/to/repo'
export DEVELOPA_API_TOKEN="$(openssl rand -hex 32)"
./bin/server
```

The example disables TLS for a local development connection. Configure TLS for connections across untrusted networks. The `.env.example` database hostname `postgres` is for Compose, not a native process; the native binaries do not automatically load `.env`.

The native server binds to `127.0.0.1:8080` by default. Use your generated `DEVELOPA_API_TOKEN` on the unlock screen. Do not expose this foundation directly to the public internet: authentication is a single operator token, not individual accounts, and TLS-offloading proxy origin configuration is not implemented.

## Explore and track

- Browse searchable file blocks and filter by declaration kind. Search, counts, ordering, and pagination execute in PostgreSQL.
- Open a file, then a declaration to inspect its signature, documentation, parameters, returns, fields, and source position.
- **Captured implementation** renders Go syntax with physical line numbers, preserved indentation, copy/wrap controls, and an expanded reader. Truncated excerpts stay labeled; highlighting runs locally without AI or new API calls.
- **Describe function with AI** saves a description beside its source comments, with optional supported parameter notes. **Summarize callees with AI** reviews one small page of direct callees, reusing per-function cached results. The [function review API](docs/function-reviews-api.md) also supports paged repository-wide review without requiring the UI; browsing never runs inference.
- Use **Follow chain** or **View callers** on a function to navigate resolved local bindings. Bounded traversal preserves discovery paths and marks truncation; direct call sites also show unresolved targets.
- **Code flow** shows a React Flow canvas from roots to callees; **View code flow** traces upward from a selected function first. Selecting a saved feature opens its evidence flow. Shared functions link to their other callers; recursive connections are marked. The same [flow API](docs/flow-api.md) supplies all graph facts to agents without the browser. Flow explanations are explicit, cached answer requests, never automatic.
- **Feature flow** includes a searchable picker for switching saved features without leaving the flow. Results are fetched in pages from the selected snapshot; searching and switching do not run AI. Feature, function/connection, symbol-kind, and editor dropdowns open a full-width search field and support keyboard selection. Traversal depth remains a basic select.
- Map your local checkout in **Editor settings** to create VS Code or Cursor links. The client path can differ from the server's Docker path. Source columns are UTF-8 byte columns; local edits can shift saved positions.
- The server captures on startup and polls every `WATCH_INTERVAL` (default `2s`). Unchanged captures skip parsing/writes. Independently, the browser refreshes routine project data every two minutes. **Reindex** queues an immediate manual scan and temporarily refreshes progress faster; it does not complete synchronously.
- A new-publication notice keeps the current view pinned until you choose **Show latest**. Old snapshots remain readable. Deleted files do not silently resolve to newer contents.
- **Changes** shows additions, modifications, and deletions relative to the last successful capture. A first/restarted capture without a prior in-process manifest explicitly reports an unknown comparison baseline.
- **Analysis** shows syntax/call diagnostics, exclusions, and analysis limitations separately. **Features** reads saved, inferred capabilities with citations, model identity, coverage and durable job status. Browsing does not call Ollama.

Use the searchable **Workspace** dropdown to switch repositories, and **Add workspace** to browse allowed folders. Registrations and checkout roots are saved in PostgreSQL and restored on server restart. Configure `WORKSPACE_ROOTS` to allow folder selection; `REPOSITORIES` or `REPOSITORY_PATH` can seed initial checkouts. See [multiple repositories](docs/repositories.md) for native/Docker setup and the repository-scoped API. The browser cannot grant the server access to arbitrary directories.

Agent clients that know an engine-visible checkout root can resolve it without listing or guessing workspace names: `POST /api/repositories/resolve` with `{"path":"/absolute/repository/root"}` returns the repository identity and latest snapshot. The path is canonicalized but never echoed. Docker callers must use the mounted container path; host paths are not automatically translated across filesystem namespaces.

Scans are serialized per repository and bounded by `SCAN_TIMEOUT` (default `30s`). Each tracker keeps its own change baseline, and a failed update retains that repository’s previous published snapshot. Snapshot IDs identify publications; a revert to previously seen code gets a distinct ID and change record.

See the [implemented API reference](docs/explorer-api.md) and [generated OpenAPI 3.1 contract](api/openapi.json). The running engine serves the same document at **`GET /api/openapi.json`**, without authentication. [Generation and agent usage](api/README.md) explain how to keep it current.

## Enable intelligence

Install a suitable local model in your own Ollama instance, then configure the server:

```sh
export OLLAMA_URL='http://127.0.0.1:11434'
export OLLAMA_MODEL='your-installed-local-model:tag'
export OLLAMA_CLOUD=false
unset OLLAMA_API_KEY
export OLLAMA_TIMEOUT='60s'
export AI_TIMEOUT='120s'
./bin/server
```

Keep the repository/database/token variables from the native example. For local Docker-to-host inference, explicitly set `OLLAMA_URL=http://host.docker.internal:11434` and configure your host Ollama to accept the container connection, or use a private Ollama service URL. Unconfigured model roles disable their inference without disabling structural indexing or context/call APIs. No Ollama service, model download, Redis, or automatic cloud fallback is bundled.

Local mode checks installed-model metadata before sending source, rejects cloud-backed aliases/public addresses/API keys, disables proxies/redirects, and pins the selected weights digest. Operator-controlled private networks are allowed; use TLS across untrusted networks. `OLLAMA_NO_CLOUD=1` on the Ollama server adds its own cloud-disable policy. Changing a pinned model requires restarting Developa; partial feature generations cannot mix providers or model revisions.

### Explicit Ollama Cloud

Cloud mode sends selected source excerpts and questions to Ollama. Enable it only for repositories you authorize for external processing. Load `OLLAMA_API_KEY` into the server environment from your shell or secret manager; never commit it, paste it into the UI, or include it in a URL.

```sh
# OLLAMA_API_KEY must already be exported in this shell.
export OLLAMA_CLOUD=true
export OLLAMA_BASE_URL='https://ollama.com/v1/'
export OLLAMA_ANALYSIS_MODEL='gpt-oss:20b'
export OLLAMA_ANSWER_MODEL='deepseek-v4-flash:0731'
./bin/server
```

Use an exact model returned by Ollama's cloud model listing, without a local `-cloud` alias suffix; unlisted bare aliases cannot establish the required revision provenance. `OLLAMA_BASE_URL` is an alias for `OLLAMA_URL`: `/v1/` or `/api/` is normalized to the origin because Developa uses Ollama's native API. Conflicting values are rejected. The cloud adapter accepts only the HTTPS `ollama.com` origin, sends Bearer authentication, and disables proxies/redirects. Merely setting a key cannot enable cloud mode. The key is never exposed to the browser, stored in the index, or exported in telemetry. Compose forwards the same settings.

Ollama Cloud currently does not support enforced structured outputs. Developa includes the schema in the prompt, omits the unsupported `format` parameter, and validates returned JSON and evidence before publication. Malformed output fails without replacing earlier results. Cloud identity records the provider-reported revision as `model@cloud:revision`; this is not a reproducible local weights digest. See Ollama's [cloud API](https://docs.ollama.com/cloud), [authentication](https://docs.ollama.com/api/authentication), and [structured-output limitation](https://docs.ollama.com/capabilities/structured-outputs).

### Durable background indexing

`OLLAMA_ANALYSIS_MODEL` selects background inference; `OLLAMA_ANSWER_MODEL` selects question answering. Each falls back to `OLLAMA_MODEL` when unset, so existing configurations still work. Roles use independent service gates: background work does not occupy the answer service's execution gate. No model selector LLM is needed. The example uses a smaller analysis model and a separate answer model; choose available models appropriate to your code and quota.

Normal indexing uses Go/Git tooling, not an LLM. Source comments supply the default descriptions. **Feature discovery is explicit by default**: with an analysis model configured and `AI_INDEX_ENABLED=true`, use **Queue analysis** to generate saved features in the background. Opening files, functions or Features never invokes inference.

Optional `AI_AUTO_FEATURES=true` enables automatic discovery when a **clean, complete snapshot with a Git commit** is published. A durable per-repository commit ledger prevents repeat automatic admission for the same commit. Working-tree edits, repositories without a commit, and incomplete captures do not trigger automatic model calls. No Git hook blocks a commit. This tracker observes snapshots on a polling interval; it does not promise to capture every intermediate commit if commits/edits happen between polls. Turning the option off pauses pending automatic jobs while explicit manual jobs remain eligible.

The worker polls every `AI_POLL_INTERVAL` (default `2s`, maximum `1m`), processes one bounded page of up to 8 symbol records from a single file with a 16 KiB evidence budget, saves its inferred features, and schedules the next page until coverage is complete. Editing the working tree while that commit is analyzed does not cancel its job; a newer commit can supersede obsolete automatic work. Manual queueing can explicitly analyze dirty or historical snapshots.

This first queue uses PostgreSQL row leases and `FOR UPDATE SKIP LOCKED`, so there is no extra NATS or Redis service. Work and checkpoints survive restarts. Expired leases are reclaimed; stale workers cannot publish through an expired lease. Consecutive failures have bounded retries/backoff. New snapshots supersede obsolete automatic work; manually queued historical snapshots remain explicit requests.

```mermaid
flowchart LR
  Button[Explicit Generate request] --> Queue[PostgreSQL job queue]
  Git[Git tracker] --> Publish[Clean commit: optional automatic admission]
  Publish --> Queue
  Queue --> Worker[Leased page worker]
  Worker --> Lookup{Cached page?}
  Lookup -- yes --> Saved
  Lookup -- no --> Ollama[Ollama]
  Ollama --> Cache[Validated page cache]
  Cache --> Saved
  Ollama --> Saved[Validated Features in PostgreSQL]
  Saved --> Read[Read API and UI]
  Worker --> Queue
```

**Queue analysis**, **Resume analysis**, and **Rebuild analysis** only submit durable work and return HTTP 202. Closing the tab does not cancel accepted jobs. **Features** subscribes to stored job progress through SSE without replacing the cards or resetting scroll. **Refresh saved features** loads newly persisted pages when you choose; queueing and read failures leave the displayed results intact. A retry can resume persisted coverage instead of re-reading earlier pages. `AI_INDEX_ENABLED=false` pauses worker execution/admission while leaving saved results and answer/context APIs available; restart with it enabled to resume. Cloud opt-in also enables these background source transfers, which may consume provider quota while no UI is open.

Opening **Features** reads saved analysis first. If the current snapshot has no generation, the UI opens the newest analyzed source snapshot and labels/pins that source explicitly. **Show latest** and explicit snapshot links stay on the requested source, with a link to saved analysis when available. No model call is needed to discover or view these results.

For agents, `GET /api/snapshots/{id}/features` still returns only that snapshot's run and evidence. When it has no run, an optional `saved_snapshot` object identifies the newest analyzed snapshot in the same repository. Follow its `id` with a second scoped Features GET; the hint is not analysis of the originally requested source. Pagination and search never substitute older feature records into the current response.

Validated page output is cached within each repository by exact bounded source input, prompt/schema version, inference policy and verified model revision. Metadata-only model checks use no inference tokens. Cache hits are validated again and citations rebound to the new snapshot's exact positions. Pages stay within file boundaries so insertion in one file does not shift every later file's cache. Changes inside a large file may still shift that file's later pages and cause misses. A changed model, prompt, or source input invalidates the relevant key; cache misses alone invoke the model. Feature metadata exposes cumulative `cached_batches` and `model_calls` for saved generation progress; these are not a billing-token meter. Cached output is still an inference, not proof of unchanged runtime behavior.

Syntax/source truncation can still make a fully visited generation partial. This is batched inference, not a global semantic proof; overlapping descriptions are not yet merged across batches. A server can track multiple checkouts; the leased queue does not make Git tracking distributed.

### Questions

Free-form questions remain **API-only**. The UI also offers a focused **Explain with AI** button on declarations and saved features. It uses the same API, generates only when clicked, persists its cited answer and reuses matching validated inference. There is no automatic LLM pass over every declaration.

```sh
curl -H "Authorization: Bearer $DEVELOPA_API_TOKEN" \
  'http://127.0.0.1:8080/api/snapshots/SNAPSHOT_ID/context?q=load%20repository&limit=8'
curl -H "Authorization: Bearer $DEVELOPA_API_TOKEN" -H 'Content-Type: application/json' \
  -d '{"question":"What does this function do?","symbol_id":"SYMBOL_ID"}' \
  'http://127.0.0.1:8080/api/snapshots/SNAPSHOT_ID/answers'
```

Replace the uppercase IDs with returned snapshot/symbol IDs. Use `feature_id` instead of `symbol_id` to explain a saved feature from its canonical source evidence; a request cannot select both. Without either selector, answers use bounded PostgreSQL full-text retrieval. Matching questions/evidence/model/prompt reuse saved inference (`cached: true`) with current source citations. The service can abstain; citation membership does not prove generated prose is correct. Feature and answer writes commit with audit/outbox records. The Ollama adapter follows the [chat API](https://docs.ollama.com/api/chat) and [structured-output schema protocol](https://docs.ollama.com/capabilities/structured-outputs).

### SSE delivery

The UI subscribes to `GET /api/snapshots/{id}/events` for durable analysis-job updates. Focused explanations use `POST /api/snapshots/{id}/answers/stream`, which emits progress/keepalives and an `answer` event after validation and database persistence. Existing JSON endpoints remain available. Streams use Bearer headers, never access tokens in URLs, and are scoped to the selected snapshot. Reconnecting a job stream reads current stored state; it does not replay a durable event log or start inference.

Queueing Features pins `?snapshot=ID` in the page URL so reloads return to that job's source snapshot. **Show latest** clears the pin. A snapshot link still requires the server access token and normal repository authorization; it carries no credentials.

SSE is a delivery channel, not the database writer: the application validates model output, commits it, then publishes the saved result. This version streams job state and completed answers, **not raw model tokens**. Ollama's native streaming format is [NDJSON](https://docs.ollama.com/api/streaming); the structured-output adapter currently requests a bounded complete JSON response. Disconnecting an answer stream cancels its request; disconnecting from job events leaves previously accepted background jobs running.

## Scan a local repository

The CLI requires Go 1.26+ to build and Git on `PATH`. Pass the root of an existing working-tree checkout. It does not clone, fetch, modify, or build the repository.

```sh
make build
./bin/developa scan --repo /absolute/path/to/repo > /tmp/developa-scan.json
./bin/developa scan --repo /absolute/path/to/repo --watch --interval 2s
```

One-shot scanning emits one JSON report. Watching emits an initial report and then one JSON line per reconciled change. It combines Git metadata/diff with file content hashes so repeated edits to an already dirty file are detected. This first watcher polls and reparses the eligible Go files after each changed snapshot; it is not yet a filesystem-event or package-incremental indexer.

Each report includes:

- Execution/trace identity and snapshot fingerprint, commit, branch, HEAD tags, dirty state, and source exclusions.
- File blocks with package, imports, deterministic declaration-count overviews, and symbols.
- Functions/methods, parameters/results, type parameters, structs/fields/tags, interfaces, aliases, named types, constants, variables, and closures.
- Documentation, signatures, source hashes, parent links for contained members, and physical file/line/byte-column spans.
- Bounded implementation excerpts, static local call bindings, unresolved call sites and call-analysis diagnostics.
- Explicit syntax completeness, diagnostics, skipped files, and analysis limitations.

Reports contain source-derived signatures, comments, initializer expressions and declaration source excerpts (up to 8 KiB each, explicitly marked when truncated). Treat reports and the database as private code artifacts. Store output outside the scanned checkout, or add the output location to `.gitignore`, to avoid watching the scanner's own output. Complete captured files are not serialized wholesale.

### Current analysis boundaries

The parser inventories syntax from all supplied Go files. A separate `go/types` pass resolves provable local function/concrete-method bindings using captured files and module paths, with no repository execution, fetching or external API stubs. It excludes test/platform/build-constrained files from typed selection and leaves their call sites unresolved. Interface dispatch, function values, unknown external declarations and invalid/ambiguous bindings remain explicit. Cross-module workspace/replacement selection is not guessed, and integer sizes use a fixed gc/amd64 policy. A resolved edge identifies a source binding; it does not prove runtime execution or a successful build.

Local named declarations are not indexed. Logical symbol IDs include the file path, so moves change identity. Syntax completeness and `call_analysis` status are separate. Existing catalogs automatically reconcile once when the index version changes, even if source bytes are unchanged.

Capture uses two matching bounded manifests for optimistic consistency and retries concurrent changes. Renames are reported as add/delete pairs. Symlinks and known secret filenames are intentionally excluded; submodules and merge conflicts make source capture partial. Filename exclusions are not a complete secret detector. Use trusted local checkouts for this foundation; isolated workers for hostile repositories are still future work.

CLI defaults are 2 MiB per file and 64 MiB total source bytes, configurable with `--max-file-bytes` and `--max-total-bytes`. Exceeding a limit fails the scan instead of silently returning a truncated index. Untracked files honor Git ignore rules; tracked files remain eligible even if ignored later.

## Telemetry

The server, PostgreSQL connection/readiness checks, Git capture/watch, parser, and CLI execution emit OpenTelemetry spans. HTTP responses include `X-Trace-ID`. W3C trace context is propagated; baggage is not. Source, queries, credentials, and panic payloads are not exported in spans.

By default, spans go to stderr, keeping CLI JSON on stdout. Set `OTEL_EXPORTER_OTLP_ENDPOINT` to an operator-controlled OTLP HTTP collector base URL to export remotely. No collector or model service is bundled yet. Publication, latest-pointer update, audit event, and outbox record commit atomically in PostgreSQL. Manual scan admission and outcomes also have durable audit records. Outbox dispatch and retention are not implemented; pending rows do not prove remote telemetry delivery.

The accepted structural scan queue is process-local; restart reconciles the checkout again. Ollama analysis jobs are separate, durable PostgreSQL work with leased execution and saved-page recovery. Failure-outcome auditing makes a bounded best-effort attempt if PostgreSQL is unavailable. Run one engine process for a workspace group, with up to 32 monitored repositories; distributed source tracking, retention, and pruning are future work.

## Checks

```sh
npm ci --ignore-scripts  # frontend/API check tooling; no production Node runtime
make check         # formatting, vet, complexity <= 10, tests, OpenAPI validation/freshness
make race          # race detector across product packages
DEVELOPA_TEST_DATABASE_URL='postgres://user:password@127.0.0.1:5432/testdb?sslmode=disable' \
  make integration
```

Git tests create real temporary repositories. PostgreSQL/HTTP integration tests skip when `DEVELOPA_TEST_DATABASE_URL` is absent and fail if a supplied database is unreachable. Use an isolated test database with permission to create/drop test schemas. Tests cover snapshot isolation, rollback, fixed query budgets, and the complete Git → parser → PostgreSQL → HTTP flow. The pinned Go and JavaScript complexity tools check implementation and tests with a maximum of 10. Frontend checks run on Node 22.11+; production embeds static assets in the Go executable.

See [implementation status and verification](docs/implementation-status.md) for the checked scope and remaining work.

## Design artifacts

- [Product and architecture](docs/architecture.md): scope, Go analysis, evidence, storage, UI, security, and implementation sequence.
- [Engineering requirements](docs/engineering-requirements.md): complexity, OTEL/auditing, query discipline, separation of concerns, and testing gates.
- [Generated OpenAPI contract](api/openapi.json): implemented workspace, snapshot, symbol, call/flow, feature, context, answer/review and SSE endpoints, including default-workspace compatibility aliases.

## Core principle

Extract structural facts with language tooling. Generate explanations from those facts and cited source. Keep inferred product behavior visibly separate from resolved code relationships. Every answer refers to an identifiable source snapshot.

## Next implementation milestone

Add dependency/version evidence, broader build-aware resolution, cross-batch feature synthesis and MCP. Operational follow-ups include standalone workers, outbox delivery, retention, TLS-proxy origin policy and multi-user authorization.
