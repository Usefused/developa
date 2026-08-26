# Implemented HTTP contract

[`openapi.json`](openapi.json) is the generated OpenAPI 3.1 specification. It includes the running `/api` endpoints, health checks, shared bearer authentication, request limits, nullable responses, pagination and SSE. It replaces the unimplemented `/v1` draft. UI routes are not part of this API contract.

The Go binary embeds this file and serves it publicly at **`GET /api/openapi.json`**. Discovery performs no database reads or model calls. API data still requires `Authorization: Bearer <DENVERR_API_TOKEN>`.

## Agent workflow

See [source navigation guidance](../docs/agent-context.md) for following helper implementations, interface candidates, and full source chunks without overstating analysis completeness.

1. Read `/api/info`, authenticate, and list `/api/repositories`.
2. Read `/api/repositories/{repository}/project` and retain its `snapshot.id`.
3. Use `/api/repositories/{repository}/snapshots/{snapshot}/symbols`, `/context`, `/calls`, or `/flow` for bounded retrieval. Follow pages or recenter traversal instead of fetching the whole repository.
4. Read cached `/features` and `/function-reviews`. Use `/answers/lookup` to retrieve a saved explanation without inference.
5. Only on explicit demand, POST `/answers`, `/function-reviews`, or `/features/generate`. Feature generation returns durable job admission; poll `/analysis-job` or subscribe to `/events` for saved progress.

All paths after step 2 remain repository- and snapshot-qualified. The `/api/project` and `/api/snapshots/{snapshot}/...` aliases use the engine's first workspace; agents should prefer the explicit repository path. Capability flags describe configuration, not live provider readiness. An empty engine or disabled component can return `503`.

## Maintaining the contract

```sh
npm ci --ignore-scripts
make api-generate
make api-check
go test ./internal/openapi ./internal/transport/http -run 'TestOpenAPI|TestSchema'
```

Response schemas come from the actual Go JSON types. Route descriptions, query bounds and decoded request constraints live in `internal/transport/http/openapi*.go` beside their handlers. Update those when request validation changes, then regenerate; do not edit JSON by hand. Route inventory and embedded-spec freshness tests prevent missing routes or stale checked-in output. `make check` and the tagged release workflow include contract checks.

Lengths marked `x-max-utf8-bytes` are enforced by the server in UTF-8 bytes; JSON Schema's `maxLength` counts characters. Request bodies also have an independent encoded-byte cap. Bodies reject unknown fields and trailing JSON. The spec documents canonical typed requests; the Go JSON decoder may additionally accept `null` as an omitted scalar. File/folder paths are validated against the repository or allowed root at runtime.

## SSE

Operations returning `text/event-stream` define their JSON event payloads under `x-sse-events`. Standard OpenAPI clients generally expose these as a stream/string; clients must parse `event:` and `data:` frames themselves. `started` acknowledges a foreground execution; `answer` and `reviews` contain completed results saved before delivery. `analysis` reports persisted background job state. `error` carries a safe HTTP status number and trace ID; comment heartbeats are not payloads.

This is not provider-token streaming. There is no durable event replay or `Last-Event-ID` cursor. A read-only subscription can reconnect for current state. Do not automatically replay a disconnected inference POST: it may spend another model call. Static call relationships and inferred features are not proof of runtime execution order.
