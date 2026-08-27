# Reading code through the Denverr API

Use the API to collect source evidence before explaining behavior. Static relationships, possible implementations, and inferred descriptions have different strengths; keep those distinctions in your answer.

## Establish the source scope

Reuse the supplied or cached API contract. Only fetch `GET /api/openapi.json` if the contract has not already been provided, and cache it once per server version. Authenticate data requests with the operator-provided bearer token in the `Authorization` header; do not put credentials in URLs or reports. Keep any supplied repository and snapshot IDs. When the agent knows the absolute engine-visible repository root, resolve it with `POST /api/repositories/resolve` and `{"path":"/absolute/repository/root"}`. Otherwise list `GET /api/repositories`, select the intended repository, then read `GET /api/repositories/{repository}/project`. Do not guess between multiple workspaces with similar names.

Path resolution matches an exact repository root after symlink canonicalization; it does not accept a nested working directory. The path must be visible to the Denverr process. The response contains repository identity and the latest snapshot but never echoes the checkout path.

Retain that `snapshot.id`. Use the repository-qualified prefix below for source reads:

```text
/api/repositories/{repository}/snapshots/{snapshot}
```

Do not substitute a newer snapshot midway through an investigation. If a publication changes, finish on the pinned snapshot or explicitly restart on the new one. The compatibility paths without a repository ID select the engine's first workspace, which may not be the intended project.

Read `/details` for source exclusions, diagnostics, call analysis, and completeness limitations. Configuration flags from `/api/repositories/{repository}/capabilities` do not prove that a model is currently available. A missing symbol, empty result page, or unresolved call does not by itself establish that the behavior is absent.

## Read a feature in one bounded call

Search the saved feature index with `/features?q=...`, then request:

```text
/features/{feature}/context?source_limit=20&depth=6&flow_limit=80
```

The response keeps four layers explicit: the inferred feature claim, canonical source declarations with captured bodies and physical positions, a resolved static feature flow, and limitations. This GET performs no inference. It uses a constant three set-based SQL reads, so feature evidence count does not create N+1 access. `source_limit` is bounded from 1 through 20, `depth` from 1 through 12, and `flow_limit` from 1 through 100.

Use the source declarations to inspect what the cited functions actually do. Use the nested flow to locate callers, callees, entry evidence, shared dependencies, cycles, unresolved counts, and truncation. Do not treat the feature title or summary as proof, or the flow as runtime ordering. If a retained declaration is truncated, continue through its `/symbols/{symbol}/source` endpoint.

## Find declarations and inspect their dependencies

Start with bounded `/symbols?q=...`, `/files`, or `/context?q=...` requests. Narrow a symbol search by `kind` or `file` when appropriate. Read `/symbols/{symbol}` for the declaration, signature, documentation, and source preview. Follow result pages with `limit` and `offset`; respect `total` and any explicit truncation fields.

Use `/calls?symbol_id={symbol}&direction=out` to inspect direct call sites, or `/flow?symbol_id={symbol}` and `/symbols/{symbol}/chain` for bounded traversal. An incoming search identifies callers. A bounded graph can omit relationships beyond its limits; recenter traversal on a relevant boundary instead of interpreting an omitted edge as evidence of absence.

Before asserting what a function does, inspect the implementations of helpers that determine its behavior. Follow a resolved local call's `target` source reference or `target_id` to that declaration. A wrapper's name, signature, comment, or call site alone does not establish a helper's error handling, filtering, state changes, persistence, retries, or return values. Read the relevant helper bodies and continue through their dependencies as needed for the question.

Call `resolution` describes static analysis. It does not establish runtime order, reachability on a particular input, or which branch will execute. Preserve external, builtin, unresolved, and resolved classifications when presenting the evidence.

## Follow interface dispatch as candidates

An interface call can include `interface` and `interface_method` source references while remaining unresolved. Request `/symbols/{symbol}/implementations` using the named interface ID to see all method links, or the interface-method ID to focus on that method. The response is paginated: follow `offset` until the returned items cover `total`.

Each item provides `interface`, `method`, `receiver`, and `target` references with symbol IDs, paths, and spans. Follow `target.symbol_id` to the concrete method body, and inspect the receiver declaration and construction or assignment sites that connect it to the caller. `pointer` describes the pointer method-set requirement; it does not identify a runtime instance.

Treat the evidence labels precisely:

- `go_types_method_set` establishes a static implementation relationship from available Go type evidence. It does not prove that this receiver is supplied at the call site.
- `signature_match_with_unavailable_types` identifies an unproven candidate whose signatures match while imported type evidence is unavailable. The analysis is partial; do not promote it to a proven implementation or resolved call edge.

Inspect the response's `analysis` status and limitations, including on an empty page. Historical snapshots may not contain implementation analysis and can require reindexing. An empty candidate list is not proof that no implementation exists outside the indexed or analyzable source. An existing noninterface symbol can return an empty list; missing or out-of-scope symbols return `404`.

## Read complete source when previews are truncated

Symbol and context excerpts are bounded previews. If a relevant body is truncated, read `/symbols/{symbol}/source?offset=0&limit=8192`, then request the exact `next_offset` returned by each chunk until it is `null`.

Offsets are zero-based UTF-8 byte positions relative to the declaration, not file offsets or character counts. The limit is a byte budget from 4 through 16384; chunks end on rune boundaries. Use the returned cursor rather than computing it from string character length. Append chunks in offset order and check that `snapshot_id`, `symbol_id`, `source_id`, and `content_hash` stay consistent. `span` identifies the declaration in the file; lines and UTF-8 byte columns are one-based, and span ends are exclusive.

`complete` describes whether the full declaration was retained, not whether the current chunk is the last. Follow `next_offset` even when `complete` is true. Successful legacy reads can include limitations. A `409 source_unavailable` means the requested snapshot has insufficient retained source, such as a truncated historical preview; do not claim to have inspected the missing body. Reindexing creates new evidence and does not repair the old snapshot in place.

## Form conclusions from the evidence collected

Separate observed source behavior from inference about runtime behavior. Cite the pinned snapshot, file path, and relevant declaration or lines for claims that depend on code. Explain which helper implementation supports each substantive claim. Where interface wiring is not established, state which candidate bodies were inspected and what remains unresolved.

Cached features, reviews, and saved answers can help orient an investigation, but their claims remain inferred and must be checked against their source evidence. GET reads and `/answers/lookup` do not invoke a model. Do not queue inference or scans merely to fill a gap without authorization for that work.

When source, dependency context, type evidence, or graph coverage is missing, state that limitation and narrow the conclusion. Never present partial analysis as complete or a candidate target as a runtime-resolved edge.
