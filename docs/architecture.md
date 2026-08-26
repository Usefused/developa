# Product and architecture proposal

Status: draft, 26 August 2026. This describes a proposed system, not implemented capabilities or measured performance.

## 1. Product definition

A self-hosted service that turns source code into reusable, versioned knowledge. Developers explore that knowledge through a visual file explorer; agents consume it through REST and MCP. Questions are answered by the server using indexed evidence. A chat interface is not part of the initial UI scope.

The differentiator is the connection between three things: a typed code inventory, explanations of capabilities with source evidence, and compact context retrieval that agents can reuse across sessions.

### Initial boundaries

- One language: Go. Model functions, methods, structs, interfaces, aliases, named types, fields, constants, variables, and closures. Go has no classes; leave the generic symbol model extensible for other languages.
- Ollama is the AI runtime for explanations, feature inference, and answers. Use local models; do not silently select a cloud provider or cloud-backed Ollama model.
- Expose project version/revision evidence and Go dependency inventory, versions, replacement directives, and usages through the same snapshot-scoped API.
- Inventory all eligible repository files, but semantically analyze Go first. Non-Go text may later support feature evidence. Binary, oversized, ignored, secret-bearing, and generated files follow explicit policies. Report exclusions rather than claiming complete analysis.
- Support multiple repositories with isolated snapshots, but no cross-repository call resolution in v1.
- Support local, administrator-configured source mounts first. Git ingestion follows behind an allowlisted connector.
- No code editing, arbitrary terminal execution, or test execution by the question-answering agent.
- No claim to reconstruct every runtime path. Reflection, interfaces, callbacks, plugins, cgo, configuration, and external services introduce limits.

## 2. Keep facts and interpretation separate

| Layer | Produced by | Examples | How it is presented |
| --- | --- | --- | --- |
| Structural facts | Go parser and type information | Signature, receiver, field, definition, direct callsite | Resolved for this build and snapshot |
| Candidate relationships | Static call analysis and framework adapters | Possible interface implementation target, registered handler | Candidate, with resolution method and limitations |
| Semantic claims | Optional model using retrieved evidence | “This package implements session renewal” | Inferred, with citations and review state |
| Runtime observations | Optional future tracing integration | This call occurred in a particular run | Observed for the identified run only |

Never silently replace a fact with model output. A model explanation is not proof of runtime correctness, deployability, test success, or absence of bugs. A citation proves where a claim came from; it does not automatically prove the claim itself.

## 3. Go indexer

### Pipeline

1. Capture an immutable source snapshot and content hashes. For mounted working trees, copy eligible content into staging and detect changes during capture; retry or mark the capture inconsistent. Do not analyze a moving checkout and label it a Git commit.
2. Record file inventory, exclusions, modules, workspace configuration, selected Go toolchain, build tags, GOOS, GOARCH, cgo policy, and test inclusion.
3. Parse eligible Go files for declarations and locations. Report inactive build variants without merging mutually exclusive declarations.
4. Load active packages with `golang.org/x/tools/go/packages`; use `go/ast` and `go/types` to resolve definitions, identifier uses, types, method selections, and instantiations.
5. Build direct call relationships. Add SSA/callgraph analysis when needed for possible targets of indirect calls. Preserve the callsite even when no target can be resolved.
6. Persist facts and diagnostics to an unpublished snapshot. Validate referential integrity and publish its status atomically.
7. Optionally build symbol summaries and feature claims against that exact snapshot. Structural indexing works with no LLM configured.

The loader provides typed package information; SSA callgraph tooling is a separate analysis stage. VTA is an option for candidate targets, not a guarantee of complete runtime behavior. See [Go packages](https://pkg.go.dev/golang.org/x/tools/go/packages) and [VTA](https://pkg.go.dev/golang.org/x/tools/go/callgraph/vta).

### A symbol record

Store an opaque ID, a snapshot-specific occurrence, package import path, kind, name, receiver, parent symbol, visibility, declaration signature, ordered inputs and outputs, generic constraints, documentation comment, exact source span, and content hash. Store field names, field types, embedded fields, and struct tags for aggregate types.

Preserve unnamed parameters, grouped parameters expanded into individual positions, variadic parameters, named and multiple returns, pointer receivers, generic declarations and instantiations, aliases, and anonymous functions. Signatures describe types; Go does not provide general runtime parameter validation rules through signatures.

Use a logical key based on repository, module/package, kind, receiver, and declared name, plus a separate snapshot occurrence ID. Function body edits should not change a named function's logical identity. Renames, moves, and closure identities require explicit matching or uncertainty; line numbers are not stable identity.

### References are broader than calls

Record `calls`, `reads`, `writes`, `type_uses`, `implements`, `embeds`, `imports`, and `registers_handler` as distinct relationships where supported. A function passed as a value is a reference, not necessarily a direct invocation. `go` and `defer` callsites carry their invocation mode. Type conversions are not function calls.

Each relationship records a source span, originating symbol, target when known, relation kind, and resolution (`resolved`, `candidate`, `unresolved`). Candidate targets are alternatives, not statements that every target executes. Default chain traversal follows calls only, not arbitrary reference edges.

### Returns versus HTTP responses

A function's Go return tuple is deterministic signature data. A handler's HTTP status codes, payloads, and validation behavior require body analysis and framework adapters; an `http.Handler` can return nothing while writing a JSON response. Keep these separate in the API. Do not label return types as proven HTTP response schemas.

### Dependencies and project versions

Collect manifests using Go module parsing and resolve the module build list with controlled Go tooling such as `go list -m -json all`. Persist both declared requirements and selected versions: these can differ. `go.sum` provides checksums, not a definitive list of active dependencies or a conventional version lockfile. In offline/restricted mode, return manifest-only data and diagnostics if resolution needs missing downloads; never silently enable network access. Run against staging under the existing worker/toolchain policy and do not rewrite the source checkout. Sources: [Go modules](https://go.dev/ref/mod) and [Go list](https://pkg.go.dev/cmd/go#hdr-List_packages_or_modules).

Each dependency records root-module scope, module path, direct/indirect relationship, declared requirements, selected version, resolution state, replacements, exclusions where relevant, and provenance. Handle `go.work`, multiple root modules, pseudo-versions, major-version paths, local replacements, and vendor mode explicitly. Preserve the original requested module identity alongside its effective replacement. Local replacements have a content fingerprint, not an invented semantic version. References outside approved source roots remain unresolved. Imported standard-library packages are shown separately and tied to the selected Go toolchain, not given third-party module versions.

Provide a paginated dependency detail/usage view showing which files import a package and which symbols reference its exported types/functions where type resolution succeeds. An import is not evidence that every function in the package is called. All selection, usage joins, filtering, and counts execute in SQL with bounded query budgets. Dependency source code is not automatically added as if it belonged to the first-party repository.

Project metadata keeps these concepts separate:

- **Declared release version:** supported literal version files or explicitly configured static declarations, with path/line evidence. Do not execute a build script to obtain a version. A main module's own release version is not declared by its module path or the `go` directive.
- **Git identity:** indexed commit, branch when available, tags pointing exactly at that commit, and dirty state. Retain multiple applicable tags. A tag on a dirty checkout is not an exact release identity for the edited source.
- **Module identity:** module path and root directory; a multi-module repository may contain several independently versioned components.
- **Go versions:** manifest `go` and `toolchain` directives and the actual analysis toolchain; these are not the product's release version.

Do not guess “latest project version.” When there is no release evidence, show the abbreviated commit plus a dirty indicator, or “unversioned working tree” if there is no commit. Conflicting release declarations remain separate candidates with provenance. Repository metadata and Denverr's own server build version are different resources.

Diffing `go.mod`, `go.sum`, `go.work`, replacement sources, vendor metadata, and supported version files triggers metadata refresh and the appropriate analysis invalidation. Tag/ref observation can require a new metadata snapshot even when the source tree is unchanged; pin the observed ref metadata with that snapshot. An upstream dependency release alone does not change this repository's selected version. Available-updates, vulnerability, and license feeds are separate future capabilities with explicit network policy, not part of this inventory claim.

## 4. Source versions and freshness

Every read is pinned to a snapshot ID. The repository endpoint can discover the latest published snapshot, but a client pins it for a navigation session or agent task. Never change the snapshot midway through a chain or answer.

A snapshot records the commit when available, a working-tree fingerprint, build configuration, analyzer versions, eligibility policy, completeness, diagnostics, and semantic-enrichment state. Build variants are separate analysis identities. An unpublished or failed snapshot never replaces the previous usable snapshot. Partial results must be explicitly requested at publication and remain labeled partial.

Incremental analysis invalidates changed packages, relevant reverse dependencies, affected call analysis, and claims whose evidence changed. A changed exported signature or build configuration may require a wider rebuild than a changed function body. Removed symbols remove or invalidate their relationships and claims in the new snapshot.

Source spans use repository-relative paths, one-based lines, and one-based UTF-8 byte columns, plus content hashes. Clients convert columns to their editor's coordinate convention. A UI detects local checkout mismatch before opening a historical line. “Fresh” means relative to the latest observed source state, not an assertion about an unobserved remote repository.

### Live updates driven by Git changes

Use filesystem notifications for a mounted checkout, or verified provider webhooks plus an authorized fetch for a remote repository, to trigger a debounced reconciliation. Notifications are hints; Git comparison and source hashes establish the actual change set. A configurable periodic reconciliation catches missed events and watcher overflow. Remote-only ingestion cannot see uncommitted edits on a developer's machine; that needs a mounted checkout or an optional local companion. Disk watchers cannot see unsaved editor buffers.

For committed updates, compare the last indexed commit with the observed target commit using machine-readable, NUL-delimited Git diff output. Handle non-fast-forward updates by comparing the actual trees, not assuming linear history. For a working tree, combine staged and unstaged changes with untracked-file inventory. Use porcelain status and content hashes against the **last indexed file manifest**: comparing only against HEAD misses a second edit to a file already dirty when the previous index was built. Renames are heuristic; preserve evidence of old/new paths and do not assume symbol identity solely from Git's similarity guess.

Invoke Git with argument arrays, fixed flags such as `--no-ext-diff` and `--no-textconv`, validated revisions, and a sanitized configuration/environment that disables repository-defined helpers, hooks, filesystem monitors, and filters where applicable. Never mutate or stash the user's checkout. Conflicts and unavailable dependencies produce diagnostics and partial analysis rather than false success. For an unborn repository use a file manifest until a committed baseline exists. Treat submodules as explicit boundaries unless separately registered.

The update pipeline is:

```text
disk/ref notification or poll
  → debounce + reconcile
  → capture target + diff against indexed manifest/tree
  → invalidate affected packages, relationships, and claims
  → analyze into a new snapshot
  → atomic publish + durable change event
  → API/MCP clients choose the new snapshot; UI receives publication event
```

Give every observed source state a monotonic generation within its tracked stream. Serialize or lease index publication per stream/build profile. Coalesce pending work to the latest source state. A slower job must not publish over a newer completed generation. Keep the prior published snapshot readable during indexing; if it is behind the observed generation, show it as stale. Structural publication can precede semantic enrichment; new-snapshot feature text with outdated evidence is marked stale until recomputed.

Expose tracking mode/state, observed generation, indexed generation, last observation time, queued work, and errors. Publish authenticated, repository-scoped events with sequence IDs and replay cursors for `source.changed`, `index.started`, `snapshot.published`, `features.updated`, and failures. SSE is sufficient for the explorer; REST remains the source of truth. On expired replay history, require a state refresh. Git diff provides change detection, not an instant-update latency guarantee: measure capture/index/publication lag and disclose it.

See [Git diff](https://git-scm.com/docs/git-diff) and [porcelain status](https://git-scm.com/docs/git-status). These tools expose changes; the debouncing, manifest, generation, and reconciliation policies above are proposed product behavior.

## 5. Features: capabilities supported by evidence

Feature discovery starts with entrypoints and observable effects, rather than asking a model to summarize every file independently. Initial signals include Go entrypoints, HTTP route registrations where an adapter exists, CLI commands, job registrations, exported service methods, persistence operations, and tests.

The service collects bounded relationship paths and source excerpts around these signals. A model proposes feature names, a description, preconditions, and claims. It returns existing symbol/evidence IDs, never invented source paths. Validation checks snapshot membership and source spans; substantive claim checking and optional human review remain separate.

Each feature contains:

- Name and description; status such as proposed, reviewed, rejected, or stale.
- Individual claims linked to evidence, including entrypoint, implementation, and test references when present.
- Related symbols, paths through the code, configuration requirements, and analysis limitations.
- Model/prompt version and evidence fingerprint for invalidation.

“A password reset handler exists” and “password reset is enabled in production” are different claims. Source alone often supports only the first. Tests present in the repository are supporting code, not evidence that those tests passed.

Features can overlap. Files do not have to belong to one feature. Owners can rename, merge, approve, or reject candidates; preserve human decisions as history while marking changed supporting evidence for review. No unsupported confidence percentages.

## 6. API and MCP are the primary interface

The [generated REST contract](../api/openapi.json) covers the implemented `/api` endpoints only. The following operations describe the broader design target, including capabilities that remain unimplemented:

| Operation | Purpose |
| --- | --- |
| Register repository and request snapshot | Schedule indexing against an approved source |
| Get job and snapshot | Inspect progress, failures, coverage, build profile, and freshness |
| Inspect tracking / subscribe to events | Observe source generations, index lag, and published updates |
| Inspect project / list dependencies and usages | Read release/revision evidence, toolchain metadata, module versions, and where dependencies are used |
| List files / inspect file | Supply the visual block overview and contents |
| Search symbols / inspect symbol | Return typed signatures, fields, source location, and a cited summary |
| List references | Find usages, with relationship kind and callsite |
| Follow chain | Bounded upstream/downstream call traversal |
| List / inspect features | Return capabilities and their supporting claims/evidence |
| Retrieve context | Return selected symbols, relationships, claims, and excerpts within a byte budget |
| Create / retrieve answer | Run an asynchronous, read-only question-answering task |

All data is repository-authorized and snapshot-scoped, including indirect graph expansion and citations. Pagination cursors are opaque and bound to snapshot, filter, and ordering. Chain responses have depth/node/edge ceilings, cycle markers, external-boundary records, and a `truncated` indicator. A static chain indicates possible relationships, not execution order or guaranteed reachability in production.

REST and MCP call the same service layer and authorization logic. Initial MCP tools: `get_repository_context`, `get_project_info`, `list_dependencies`, `get_dependency`, `find_dependency_usages`, `find_symbols`, `get_symbol`, `find_references`, `follow_chain`, `list_features`, `get_feature`, `retrieve_context`, and `ask_codebase`. Index management is separately authorized and not part of the default read-only toolset.

MCP provides access to persistent knowledge, not permanent memory inside the model. Prefer deterministic retrieval to invoking the internal answering agent for every external agent question. This avoids unnecessary nested model calls and token costs.

### Internal answering service

Authenticate → pin snapshot → retrieve symbols/features → expand relevant relationships within limits → read cited source excerpts → generate answer → validate citation IDs and spans → return answer, evidence, unresolved questions, and usage metadata.

The model has no filesystem, shell, outbound browsing, or source mutation tools. Repository comments and docs are untrusted input, never operational instructions. Enforce per-request tool, time, token, and output budgets. If evidence is missing, respond with a partial answer or `insufficient_evidence`; do not fill gaps from assumptions.

### Ollama integration

Ollama is the selected AI runtime. The application calls its native HTTP API from a dedicated Go adapter: `/api/chat` for summaries, feature claims, and answers, and optionally `/api/embed` if semantic retrieval is enabled. Use schema-constrained responses for claim/evidence records and independently validate them before persistence. Explicitly choose streaming or non-streaming behavior; never assume a streamed response is a single JSON document. Sources: [Ollama chat](https://docs.ollama.com/api/chat) and [embeddings](https://docs.ollama.com/api/embed).

Operator configuration specifies the endpoint, generation model, optional embedding model, timeouts, concurrency, context limits, output limits, and approved model identity. Store Ollama version, model name/digest, generation settings, prompt version, source snapshot, and evidence fingerprints with generated artifacts. Model tags can change; a model change invalidates affected semantic caches. Embedding model/dimension changes require a new embedding generation rather than mixing incompatible vectors. Exact signatures, relationships, dependencies, and version evidence remain independent of Ollama.

Keep inference local: configure Ollama with `OLLAMA_NO_CLOUD=1`, approve only local model artifacts, and apply an egress policy. Provision model weights explicitly; do not let repository content or normal question requests trigger model pulls, provider changes, or arbitrary URLs. Local-only settings do not by themselves prevent outbound model downloads or other application traffic. See [Ollama's local-only configuration](https://docs.ollama.com/faq).

Only the backend can reach Ollama. Do not expose it directly to the browser or publish its unauthenticated inference endpoint on an untrusted network. A remote operator-owned Ollama instance requires an approved network boundary and authenticated/TLS proxy where appropriate. Separate foreground answering capacity from background enrichment and bound both queues. On model unavailability, overload, timeout, or invalid output, return a clear capability error or job failure without a cloud fallback; structural endpoints remain available. OTEL records model identity, durations, usage counts, errors, and execution correlation, not prompts, source, responses, or hidden reasoning by default.

## 7. Storage and deployment

### Start with PostgreSQL

Use relational tables for repositories, snapshots, project-version evidence, modules, dependency requirements/resolutions/usages, files, symbols, parameters/results, callsites, edges, features, claims, evidence, summaries, jobs, answer requests, and source blobs. Composite foreign keys include repository/snapshot identity so cross-snapshot edges cannot be inserted accidentally.

Use indexed edge tables and bounded recursive queries for traversal; full-text search for symbol names, docs, and feature text; JSONB for language-specific detail and analysis metadata. PostgreSQL supports [recursive queries](https://www.postgresql.org/docs/current/queries-with.html) and [full-text search](https://www.postgresql.org/docs/current/textsearch-intro.html). Whether this meets the product's eventual scale must be benchmarked.

Add optional pgvector only after retrieval evaluation shows value beyond exact and lexical search. A separate graph database is not required merely because the data contains relationships. Benchmark traversal depth and fan-out before reconsidering storage.

Keep durable jobs in PostgreSQL with leases, retries, idempotency keys, recovery, and one active index job per repository/build profile. Redis is optional later for disposable caches, rate limits, or distributed coordination. It is never the only copy of the index or queue state. Cache keys include authorization scope, snapshot, analyzer version, and request parameters.

### Deployment shape

```text
REST clients / MCP agents / optional explorer
                    │
           Go API + shared services
                │          │
           PostgreSQL    Ollama (local models)
                │
         Isolated index worker
                │
     Approved source → immutable staging
```

Denverr ships first as one native binary. `denverr serve` owns the API, embedded UI, repository watchers and leased analysis worker for small trusted deployments; PostgreSQL and Ollama remain operator-managed services. On every push to `main`, CI creates the next patch tag and GoReleaser produces the platform archives. A future separate worker command can create a stronger isolation boundary for untrusted repositories without changing the application services or API.

Define PostgreSQL retention, backup, restore, migrations, health checks, and graceful job shutdown. Denverr reads local source without modifying it. Operators should run the process as an unprivileged account with access limited to approved workspace roots.

## 8. The visual explorer

Use a stable two-dimensional arrangement with subtle building/block depth rather than a freely rotating 3D city. Folders/packages form labeled groups. Each file is a readable block showing its name, short purpose, and contained symbol counts. A block opens into a list of functions, types, and fields.

Selecting a function opens a detail sidebar containing signature, inputs, outputs, receiver, explanation, evidence, callers, and callees. Selecting Follow chain changes the focus to three expandable lanes: Used by → Selected symbol → Calls. Reveal another hop on demand. Show branching explicitly and avoid drawing every repository relationship at once. Reference usages are a separate view from call chains.

The Features tab is a capability catalog. Opening a feature shows its claims and source evidence; selecting evidence enters the same symbol detail view. A Dependencies tab lists modules, direct/indirect relationships, selected versions, replacements, and usages. A project overview shows release evidence, commit/branch/dirty state, root modules, and Go toolchain metadata. These are planned UI additions; the initial interactive concept only demonstrates Code blocks and Features. Show the pinned snapshot and stale/partial state consistently. The UI does not implement retrieval or answer logic; all content comes from the public API. No question composer in the initial UI.

Keyboard access, a plain list alternative, readable labels, stable selection, responsive layouts, and progressive loading are required. The building metaphor must not obscure source paths or navigation.

### Opening editors

The server returns a source locator, not a command to execute. A user-controlled client mapping translates repository-relative paths to a local checkout root. VS Code supports file URIs and `code --goto`; see its [CLI documentation](https://code.visualstudio.com/docs/configure/command-line).

A remote server cannot open an editor on the visitor's computer. Use explicit user-clicked deep links for supported editors, or an optional local companion for editors without a usable URI handler. Provide a copy-path-and-line fallback. “Any editor” is an adapter interface, not an initial guarantee. URI generation percent-encodes paths, permits only known schemes, and never accepts an arbitrary shell template from a repository.

## 9. Security and correctness requirements

- Authenticated APIs with repository-scoped `read`, `index`, and `ask` permissions; separate administrative registration. Tokens are hashed at rest. No anonymous deployment default.
- Only approved source roots. Prevent path traversal, escaping symlinks, archive bombs, credential leakage, and SSRF in later Git connectors.
- Treat Go package loading as tool execution. Use unprivileged, resource-limited workers, controlled toolchains, stripped environments, no inherited repository-defined package driver, and a preconfigured dependency/network policy. Even without executing application binaries, build tooling and cgo can invoke processes.
- Never automatically run tests, `go generate`, repo hooks, or arbitrary build scripts. Dependency fetching and cgo require explicit operator policy.
- Repository secrets are excluded from semantic ingestion; operator-supplied credentials never enter the index or answer evidence.
- SQL deadlines, fan-out ceilings, model budgets, cancellation, and job leases prevent unbounded work.
- Do not log source excerpts, prompts, tokens, or credentials by default. Audit access and administrative actions with redaction.
- Deletion and retention remove derived summaries, embeddings, answer records, and source blobs as well as structural rows, according to reference and retention policy.

## 10. Implementation order and acceptance gates

All implementation milestones must meet [engineering requirements](engineering-requirements.md): complexity at most 10, OTEL and durable audit correlation, DRY, SQL-side filtering, no N+1 access, separation of concerns, useful rationale comments, and unit/integration tests.

### A. Structural Go slice

Implement source capture, Git-diff reconciliation, build-profile loading, project/dependency metadata, inventory, symbols, direct calls/references, PostgreSQL migrations, jobs, and read API. Deliver an OpenAPI-compatible native server and GoReleaser distribution. Verify watcher recovery, incremental invalidation, and publication ordering alongside extraction accuracy.

Fixture checks cover packages with duplicate names, receiver methods, generics, aliases, grouped parameters, multiple/named returns, embedded structs, interfaces, closures, function values, recursion, `go`, `defer`, build tags, test variants, unresolved imports, and Unicode positions. Classify missed indirect targets explicitly. Never treat a partial load as a complete index.

### B. Persistent agent context

Add bounded traversal, retrieval, and MCP. Test restart persistence, cross-repository authorization, cycle termination, output budgets, branch/build isolation, deletion, incremental invalidation, and source mismatch detection. Measure retrieval relevance and context size against direct repository exploration before claiming savings.

### C. Features and answers

Add the Ollama adapter, feature evidence, review state, and asynchronous read-only answers. Evaluate with hand-checked questions and capability claims. Measure supported claim precision, citation validity, unsupported-claim rate, and abstention on missing evidence. Attack fixtures include instruction-like comments and misleading docs. Test Ollama outages, missing models, malformed/streamed responses, timeout/cancellation, model changes, embedding mismatch, and absence of unauthorized egress or fallback.

### D. Visual explorer

Build file blocks, file contents, symbol sidebar, follow-chain lanes, feature evidence, and editor adapters against the same API. Usability checks: find a function, explain its inputs/outputs, identify a caller, trace a dependency, and verify a feature without losing orientation.

No timeline or performance promise is implied by this sequence. Test representative repositories at several sizes and retain correctness baselines before publishing capacity claims.

## Decisions still open

License, supported Go version range, initial framework adapters, Ollama model/hardware sizing, source retention defaults, and initial customer repository size. The product name is Denverr and the first distribution is a native GoReleaser archive. The AI runtime is Ollama. Model selection should follow a representative codebase evaluation and deployment memory budget.
