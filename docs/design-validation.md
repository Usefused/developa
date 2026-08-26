# API contract validation

The earlier `/v1` design draft has been replaced by the [implemented OpenAPI 3.1 contract](../api/openapi.json). Proposed features remain in [architecture.md](architecture.md), not in the served API specification.

## Repeatable checks

- `make api-generate` derives response schemas from Go wire types and applies the HTTP request bounds, authentication and SSE contract.
- `make api-check` detects stale output and runs the pinned OpenAPI validator, internal-reference resolution, unique operation IDs, and path-parameter checks.
- Go contract tests compare the specification with registered Chi routes, including dynamically mounted repositories and default-workspace aliases. They check deterministic generation and the embedded public discovery endpoint without needing PostgreSQL or Ollama.
- Schema tests cover embedded fields, nullable pointers/collections, omitted and private fields, recursive types, and unsupported encodings.
- `make check` includes these gates alongside the existing backend/frontend checks. `make race` with `DENVERR_TEST_DATABASE_URL` also exercises the real Git/PostgreSQL/HTTP integration suite.

These checks establish the documented interface, not production load/security certification or model quality. See [implementation status](implementation-status.md) for verification history and remaining limitations.
