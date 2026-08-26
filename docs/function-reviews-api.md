# Saved function reviews

These implemented `/api` endpoints use the same bearer authentication, configured repository scope, snapshot IDs, origin checks, and error responses as the explorer. Their request, response and SSE schemas are included in the [generated OpenAPI contract](../api/openapi.json).

Source comments remain in `symbol.doc` / `symbol.comment`, and `symbol.documentation` compiles declaration and inline comments without AI ([source-summary contract](explorer-api.md#source-summaries)). An optional sibling `review` contains saved AI prose, optional parameter notes, model identity, source identity, truncation/abstention flags, and canonical evidence. Reviews appear in symbol detail/list and flow responses. Flow cards prefer source comments; without comments they use a supported saved AI summary, then a deterministic signature fallback. Both the comment and review remain available in the API and sidebar.

## Read and generate

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/api/snapshots/{snapshot}/function-reviews` | Read a page of functions and their saved reviews; never invokes a model. |
| POST | `/api/snapshots/{snapshot}/function-reviews` | Review one bounded page and return it only after validation and audited persistence. |
| POST | `/api/snapshots/{snapshot}/function-reviews/stream` | Same operation over SSE: `started`, then a saved `reviews` page or a safe `error`, with heartbeat comments while waiting. |

GET query parameters and POST JSON fields share the following options:

```json
{"callee_of":"CALLER_SYMBOL_ID","limit":4,"offset":0}
```

- `symbol_id`: select one function, method, closure, or interface method.
- `callee_of`: select distinct resolved local direct callees of one callable. Repeated call sites do not duplicate functions; recursive calls may include the caller itself. External/unresolved calls are counted separately, never assigned invented implementations.
- Omit both selectors to walk all callable declarations in the snapshot. The two selectors are mutually exclusive.
- `limit`: default 4, maximum 8. `offset`: default 0, maximum 100000. Stable ordering is path, physical source line, and symbol ID.

Each page contains `snapshot_id`, normalized `options`, `items`, `total`, `next_offset`, `unresolved_count`, `model_calls`, `cached_count`, and `limitations`. `items` use the ordinary `{path, symbol, review}` shape. `review` is absent until a review is saved for that snapshot. GET counters describe this read (zero model calls), not historical generation cost.

For example, a saved review contains:

```json
{
  "symbol_id":"SYMBOL_ID",
  "source_id":"SOURCE_ID",
  "summary":"Returns the supplied name after trimming surrounding spaces.",
  "parameters":[{"position":0,"description":"The name to normalize."}],
  "insufficient_evidence":false,
  "context_truncated":false,
  "model":"MODEL_WITH_VERIFIED_REVISION",
  "prompt_version":"function-reviews-v2",
  "created_at":"2026-08-26T12:00:00Z",
  "cached":false,
  "evidence":{"symbol_id":"SYMBOL_ID","name":"Normalize","path":"name.go","span":{"start":{"line":5,"column":1,"offset":42},"end":{"line":7,"column":2,"offset":118}}}
}
```

`position` is the parser's zero-based parameter position, so unnamed/grouped/variadic inputs remain unambiguous. Notes are optional. Missing notes mean the review did not establish the parameter's purpose; they are not placeholders for guessed behavior. Model-supplied identities and positions are validated, while paths and spans come from the index. Evidence membership does not prove generated prose is correct.

## Batching, caching, and cost

A POST makes at most **one generation call**. The input budget can reduce the page below `limit`; always follow the returned `next_offset`, not `offset + limit`. `next_offset: null` means the selected callable set is exhausted. A client can submit a series of small POSTs to review a repository without one long request. The UI reviews only the current callee page when its AI button is clicked; **Next batch** performs a GET and does not start inference.

Each short review receives its own signature (including returns), captured implementation, compiled comments, and parameter positions/names/types. It does not fetch neighboring implementations or infer their behavior from names. Missing/truncated evidence is labeled. For a contextual explanation including resolved callers and callees, use the answer endpoint with `symbol_id`.

Reviews use `OLLAMA_ANALYSIS_MODEL`; freeform questions and flow explanations continue to use `OLLAMA_ANSWER_MODEL`. Existing `OLLAMA_TIMEOUT` / `AI_TIMEOUT` deadlines still apply. Batching limits work but cannot guarantee that a slow model never times out.

Validated reviews are cached independently per function, keyed by repository, exact bounded evidence, full declaration content hash, supplied parameter metadata, prompt/schema, inference policy, and verified model revision. Shared callees and different batch boundaries reuse results. Source line shifts reuse inference with citations rebound to the requested snapshot. Changed evidence/model revisions invalidate the relevant entries. Unknown model revisions do not use the inference cache. Resolving a model's revision can perform metadata requests even on a cache hit; it sends no source for inference.

Review rows and cache entries publish atomically with audit/outbox records. Failed/invalid/canceled batches do not publish partial reviews. Existing saved descriptions remain readable when a model is unavailable. A new successful review replaces the current review for that symbol in the selected snapshot; other snapshots remain unchanged.

These explicit review requests are **not background queue admissions**. Leaving the view cancels unfinished work; already completed batches remain saved. No automatic repository review or per-edit analysis was added. The existing durable Features job is unchanged. Source/callee review reads do not make any LLM calls. Cloud use remains explicitly operator-configured; source is never included in OTEL/audit payloads.
