# Denverr

Denverr is a self-hosted code intelligence engine for Go repositories. A single native binary watches multiple local checkouts, builds immutable PostgreSQL snapshots, serves an agent-oriented API, and includes a visual code explorer.

Structural indexing uses Git and Go tooling. It records files, functions, methods, parameters, results, structs, interfaces, source positions, implementation excerpts, call sites, resolved local call relationships, diagnostics, repository revision evidence, and changes. Ollama is used only for explicitly requested explanations and for optional durable feature discovery.

The browser application is compiled into the executable. Running Denverr does not require Docker, Redis, NATS, or a separate frontend server. PostgreSQL is the only required external service.

## Install

Install the latest macOS or Linux release with:

```sh
curl -fsSL https://github.com/Usefused/developa/releases/latest/download/install.sh | sh
```

The installer detects AMD64 or ARM64, verifies the archive against the published SHA-256 checksum, and puts `denverr` in `~/.local/bin`. Override the destination with `DENVERR_INSTALL_DIR`, or install a specific release with `DENVERR_VERSION=v0.3.2`.

Windows archives and manual downloads remain available from [Usefused/developa releases](https://github.com/Usefused/developa/releases).

To build from source, install Go 1.26+, Node.js 22.11+, npm, and Git:

```sh
npm ci --ignore-scripts
make build
./bin/denverr version
```

Node.js is used only to compile and check the embedded UI. Release archives contain one Go executable.

## Start Denverr

Supply an existing PostgreSQL database and run the server from a directory containing repositories:

```sh
export DATABASE_URL='postgres://denverr:password@127.0.0.1:5432/denverr?sslmode=disable'
cd ~/Code
denverr serve
```

Denverr binds to `127.0.0.1:8080` by default. On first run it generates an operator token, prints it once, and saves it in the operating system's user configuration directory with owner-only permissions. Set `DENVERR_API_TOKEN` to a value of at least 24 bytes when a secret manager should own the token instead.

Open [http://127.0.0.1:8080/](http://127.0.0.1:8080/) and enter that token. A verified token is saved under `denverr.api-token` in browser localStorage and restored on refresh. **Lock workspace** removes it. Use a trusted browser profile because scripts on the same origin can read localStorage.

Use explicit workspace roots or a different listener when needed:

```sh
denverr serve \
  --database-url 'postgres://denverr:password@127.0.0.1:5432/denverr?sslmode=disable' \
  --listen 127.0.0.1:8080 \
  --workspace-root ~/Code \
  --workspace-root ~/Work
```

`--database-url` can expose a password through process inspection, so `DATABASE_URL` is preferable outside local development. Configure PostgreSQL TLS across untrusted networks. Denverr currently uses a shared operator token rather than individual accounts, so do not expose it directly to the public internet.

Health endpoints remain public:

```sh
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
```

`/healthz` reports process liveness. `/readyz` checks PostgreSQL with a bounded timeout. Startup fails when the database, migrations, or a configured repository cannot initialize.

## Workspaces

The top-level searchable workspace selector switches between registered repositories. **Add workspace** browses folders under `WORKSPACE_ROOTS`; when no workspace environment is configured, `denverr serve` allows its current directory. The selected folder must be an existing Git checkout. The UI explains when Git is unavailable or a selected folder is not a Git repository.

Workspace registrations and canonical checkout paths are stored in PostgreSQL and restored after restart. A repository can also be registered before startup:

```sh
export DATABASE_URL='postgres://denverr:password@127.0.0.1:5432/denverr?sslmode=disable'
denverr workspace add --name API ~/Code/api
denverr serve --workspace-root ~/Code
```

The command reports whether the workspace already existed. Restart a running server after an out-of-process `workspace add`; adding from the UI becomes active immediately.

Existing environment configuration remains supported for scripted deployments:

- `REPOSITORY_PATH` and `REPOSITORY_NAME` seed one checkout.
- `REPOSITORIES` seeds up to 32 named checkouts as JSON.
- `WORKSPACE_ROOTS` sets up to 16 browseable roots as JSON.
- `WATCH_INTERVAL` defaults to `2s`; `SCAN_TIMEOUT` defaults to `30s`.

Agent clients that know a local checkout root can call `POST /api/repositories/resolve` with `{"path":"/absolute/repository/root"}`. Denverr canonicalizes symlinks, returns the repository ID and latest snapshot, and never echoes the submitted path.

See [multiple repositories](docs/repositories.md) and the [repository-scoped API](docs/explorer-api.md).

## Explore code

- Browse searchable file blocks and filter declarations with SQL-backed pagination.
- Open a declaration to inspect its signature, documentation-derived summary, parameters, returns, fields, physical source position, and captured implementation.
- Follow resolved callees or callers with bounded traversal and explicit unresolved sites.
- View code and feature flows in React Flow. Selecting a connection fades unrelated nodes, while shared functions link to their other positions.
- Open saved features with source citations, coverage, model identity, and analysis limitations.
- Open a supported editor at the exact local file and line after configuring the client checkout path.
- Request and persist a function explanation, feature explanation, flow explanation, or bounded callee summary only when needed. Matching explanations are reused until their source input changes.

The UI stays pinned to its selected immutable snapshot until **Show latest** is chosen. Routine project refresh is every two minutes; SSE updates job state without replacing feature cards or resetting scroll.

## Agent API

The generated [OpenAPI 3.1 contract](api/openapi.json) describes the implemented API and is served without authentication at `GET /api/openapi.json`. Data routes require `Authorization: Bearer <DENVERR_API_TOKEN>`. Repository-qualified paths are the stable interface for agents; default-workspace routes are compatibility aliases.

Useful entry points include:

- `GET /api/repositories` and `POST /api/repositories/resolve`
- `GET /api/repositories/{repository}/project`
- snapshot files, symbols, source, context, calls, and flows
- saved features and one-call feature context
- explicit answer and function-review requests, including SSE completion delivery

Source responses contain canonical repository-relative paths and physical line/byte-column spans. Function detail can include the bounded captured function body, so an agent does not need direct filesystem access to understand that function. Read requests never invoke a model.

See [agent context retrieval](docs/agent-context.md), [flow API](docs/flow-api.md), and [OpenAPI generation](api/README.md). MCP remains a planned transport over the same application services.

## Ollama

Working-tree indexing makes no model calls. Comments and documentation supply the default function summary. Configure separate models for background feature analysis and interactive answers:

```sh
export OLLAMA_URL='http://127.0.0.1:11434'
export OLLAMA_ANALYSIS_MODEL='your-smaller-model:tag'
export OLLAMA_ANSWER_MODEL='your-answer-model:tag'
export AI_TIMEOUT='120s'
denverr serve
```

Each role falls back to `OLLAMA_MODEL` when its dedicated setting is empty. An unconfigured role disables only that inference path. Local mode verifies installed model metadata, rejects cloud-backed aliases and public addresses, disables proxy redirects, and pins the selected weights digest.

Ollama Cloud is an explicit opt-in that sends selected source excerpts and questions to Ollama:

```sh
export OLLAMA_CLOUD=true
export OLLAMA_BASE_URL='https://ollama.com/v1/'
export OLLAMA_API_KEY='loaded-from-a-secret-manager'
export OLLAMA_ANALYSIS_MODEL='gpt-oss:20b'
export OLLAMA_ANSWER_MODEL='deepseek-v4-flash:0731'
denverr serve
```

The key is never sent to the browser, stored in the index, or exported in telemetry. Cloud responses are validated before publication. Generated features and answers keep snapshot, model, prompt, coverage, and source-evidence provenance.

Feature discovery is explicit by default. `AI_AUTO_FEATURES=true` admits background work only for clean snapshots with a Git commit. PostgreSQL stores the job queue, leases, progress, cached pages, features, and commit ledger, so jobs survive restarts and unchanged pages avoid repeat inference. There is no separate queue service. Normal indexing and UI browsing never consume model quota.

## Standalone scan

The same executable can scan without PostgreSQL:

```sh
denverr scan --repo /absolute/path/to/repo > /tmp/denverr-scan.json
denverr scan --repo /absolute/path/to/repo --watch --interval 2s
```

One-shot mode emits one JSON report. Watch mode emits the initial report and then one line per reconciled change. The scanner does not clone, fetch, modify, execute, or build the target repository. It uses bounded file and total-source limits and marks exclusions, syntax completeness, diagnostics, unresolved calls, and analysis limitations.

## Telemetry and audit

Server startup, user and agent executions, PostgreSQL operations, Git capture, parser work, background jobs, and Ollama requests emit OpenTelemetry spans. HTTP responses include `X-Trace-ID`; source, prompts, queries, credentials, and panic payloads are not span attributes. Set `OTEL_EXPORTER_OTLP_ENDPOINT` to export to an operator-controlled OTLP HTTP collector. Otherwise safe spans go to stderr.

Set `OTEL_SDK_DISABLED=true` to install a no-op provider that creates no spans and exports nothing. This switch applies to `serve`, `scan`, and workspace commands; it takes precedence over an OTLP endpoint.

Publication, latest-pointer updates, audit events, and outbox records commit atomically in PostgreSQL. Outbox delivery and retention are not implemented yet.

## Release and checks

`.goreleaser.yaml` builds the embedded UI and creates Denverr archives for macOS, Linux, and Windows on AMD64 and ARM64. Every push to `main` runs `.github/workflows/release.yml`, verifies the source, creates the next patch `v*` tag, builds the matrix, creates checksums, and publishes the GitHub release with the installer.

```sh
npm ci --ignore-scripts
make check
make race
DENVERR_TEST_DATABASE_URL='postgres://user:password@127.0.0.1:5432/testdb?sslmode=disable' make integration
goreleaser check
goreleaser release --snapshot --clean
```

Use an isolated integration database because those tests create and remove schemas and records. The quality gates enforce formatting, vet, unit/integration behavior, generated OpenAPI and UI freshness, and maximum cyclomatic complexity 10.

## Design notes

- [Architecture](docs/architecture.md)
- [Engineering requirements](docs/engineering-requirements.md)
- [Implementation status](docs/implementation-status.md)
- [Frontend architecture](docs/frontend.md)

Extract structural facts with language tooling. Generate explanations from those facts and cited source. Keep inferred behavior visibly separate from resolved relationships, and pin every response to a source snapshot.
