# Flow API for agents and renderers

Flow discovery is deterministic and needs no Ollama model. The API owns traversal, root classifications, dependency navigation, and recursion facts. The browser uses React Flow only for layout and interaction. Clients can make small scoped requests instead of downloading or reconstructing a repository graph.

## Discover, explore, then explain

1. Read authenticated `GET /api/project` and retain `snapshot.id` for the entire exploration.
2. Find a declaration through `/api/snapshots/{snapshot}/symbols?q=...`, or choose saved evidence from `/features` and `/features/{feature}`.
3. Read `/api/snapshots/{snapshot}/flow` with the selection and a small `limit`.
4. Follow `incoming_ids` / `outgoing_ids`, fetch `/symbols/{id}` for source details, or recenter `/flow?symbol_id={id}` to explore another bounded section. `/calls?symbol_id={id}&direction=in|out` supplies paginated call sites, including unresolved targets.
5. Only when needed, POST an explicit question and the same `flow` options to `/answers` or `/answers/stream`.

All routes use `Authorization: Bearer <server token>`, fixed operator repository scope, and the existing error/trace contract. `/api/capabilities` reports `flows` separately from model availability. Reads never start inference. Refreshing a flow does not change the source snapshot; deliberately read `/api/project` again to move to newer code.

## GET `/api/snapshots/{snapshot}/flow`

| Parameter | Meaning |
| --- | --- |
| no selector | Application mode: `main.main` and `init` declarations; otherwise zero-caller candidate roots; otherwise one deterministic callable component |
| `symbol_id` | Symbol mode: trace ancestors above this declaration, plus descendants below it |
| `feature_id` | Feature mode: use canonical citations from the feature's current saved generation as seeds |
| `depth` | 1–12, default 6, applied separately to the ancestor and descendant traversals |
| `limit` | 1–100 declarations, default 80; at most `4 × limit` call-site edges |

The selectors are mutually exclusive. Unknown, duplicate, empty, or invalid query parameters return 400. Unknown snapshots or selections outside the configured repository/snapshot return 404. Features without callable citations can contain isolated type/field evidence without invented relationships.

For example, these are separate, composable reads:

```text
GET /api/snapshots/{snapshot}/flow?depth=4&limit=20
GET /api/snapshots/{snapshot}/flow?symbol_id={symbol}&depth=6&limit=40
GET /api/snapshots/{snapshot}/flow?feature_id={feature}&depth=6&limit=40
GET /api/snapshots/{snapshot}/symbols/{symbol}
GET /api/snapshots/{snapshot}/calls?symbol_id={symbol}&direction=in&limit=20&offset=0
```

### Response

| Field | Contract |
| --- | --- |
| `snapshot_id` | Immutable source publication identity |
| `mode` | `application`, `symbol`, or `feature` |
| `options` | Normalized selection, depth, and limit; reusable for an explanation request |
| `seed_ids` | Selected declarations present in this bounded view |
| `nodes` | Unique `{path, symbol, description, description_source, seed, root_kind, incoming_count, outgoing_count, unresolved_count, incoming_ids, outgoing_ids}` records |
| `edges` | Resolved local call sites with caller/target IDs, path, and physical source span; several call sites may connect the same pair |
| `cycle_groups` | Groups of mutually reachable IDs in the returned graph; a singleton represents a self-call |
| `truncated` | A seed, node, edge, or traversal-depth cap omitted reachable data |
| `limitations` | Static-analysis coverage, root-discovery, and traversal boundaries |

`symbol` uses the normal declaration DTO, including signature, parameters/results and bounded captured source. `description` is a whitespace-normalized preview of at most 360 UTF-8 bytes. It prefers the compiled source summary (`description_source: source_comments`; `source_comment` remains the legacy declaration-only fallback), then a supported saved AI review (`llm_review`), then a factual signature/structural summary (`signature`). The full compiled prose and comment locations remain in `symbol.documentation`; an optional sibling `review` preserves the separate AI description, parameter notes, model identity and evidence. Flow reads never generate reviews. These same descriptions render directly inside the function cards; see the [explicit function review API](function-reviews-api.md).

`incoming_ids` and `outgoing_ids` are distinct sorted neighbors **within the returned edges**. The counts describe distinct resolved callers/callees in the **whole snapshot**, so counts can exceed the navigation arrays. Fetch another scoped flow or a call-site page to investigate those missing links. `unresolved_count` counts unresolved and external call sites, not builtin calls.

`root_kind` is `main`, `init`, `candidate`, `boundary`, or empty. `main` requires a function named main in package main. `init` identifies an initializer without reconstructing initialization order. A candidate has no indexed resolved callers; it is not a proven handler or unused function. A boundary has upstream callers outside the node slice. A closed cycle may have no root classification at all.

Traversal first discovers ancestors from all seeds, then descendants from the seeds, using one shared node budget. It does not descend through unrelated sibling branches of ancestors. Discovery edges take priority under the edge cap. The database uses bounded visited/frontier sets, avoiding repeated-path growth; dependency arrays and strongly connected cycle groups are computed on that bounded result. No query is issued per function.

This is a static call graph, **not runtime execution order**, a branch diagram, or proof that a feature works. Cycles are incomplete if a cap cut their edges. Disconnected components and unsupported dynamic/callback relationships can be absent even when `truncated` is false; read `limitations` too. A path through ancestors and descendants can span up to twice the requested depth.

## Explicit flow explanation

Both answer routes accept this JSON shape (replace the placeholder with a real 64-character ID):

```json
{
  "question": "Explain the supported paths and shared dependencies, including gaps.",
  "flow": {"feature_id": "<feature-id>", "depth": 6, "limit": 40}
}
```

Use `flow: {}` for application flow, or `flow: {"symbol_id":"<symbol-id>"}` for a selected declaration. The nested selection cannot be combined with top-level `symbol_id` or `feature_id`.

The server reloads the same scoped flow, includes canonical source and resolved relationships within its prompt budget, and drops prompt edges if either endpoint lacks supplied evidence. It discloses source/graph truncation and treats feature descriptions as inferred claims. Prompts are not executable; the model has no tools. Cache identity includes question, source facts, graph topology and classifications, options, model identity, prompt/schema and inference policy. Results are validated, cited, audited, and persisted; matching requests reuse inference. SSE publishes the saved answer, not unvalidated provider tokens.

Opening a feature or either flow mode never requests an explanation. The UI's **Explain flow with AI** button uses the same explicit answer POST available to an agent. Cloud source transfer remains operator opt-in and is disclosed before the button. MCP is not implemented yet; these HTTP contracts do not depend on the browser.

## Search and switch saved features

Feature flow includes a searchable feature picker. It reads `GET /api/snapshots/{snapshot}/features?q={search}&limit=24&offset={offset}` and offers **Load more options** until the matching results are exhausted. Search runs in PostgreSQL across the saved index, not just the first UI page. Selecting an option fetches that feature's detail and flow with the same snapshot ID, preserving traversal depth and size limits. No new analysis is started.

The UI uses the same searchable combobox for feature selection, function jumps, connection inspection, symbol-kind filtering, and editor settings. Traversal depth intentionally remains a basic native select. Each searchable dropdown opens a visible search field separate from the committed value; reopening starts an empty search, and dismissing it leaves the selection unchanged. Arrow keys move the active option, Enter selects, Escape dismisses without changing the value, and Tab moves between controls. Typed text is a search, not a new selectable value. Canceled searches and navigation cannot overwrite newer results.

## Browser build

The React diagram lives in `internal/webui/flow-source` and is loaded by the `/flow` framework route. `npm run build` generates the embedded application under `internal/webui/dist`. React, React Router, React Flow, Dagre and build tools are pinned in `package-lock.json`. Do not hand-edit generated bundles. `npm run build:check` detects stale artifacts, while lint/complexity checks apply to handwritten code. Docker compiles assets in a Node build stage; the final Go server has no Node runtime or CDN dependency. See [frontend architecture](frontend.md).
