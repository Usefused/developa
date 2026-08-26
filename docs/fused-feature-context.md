# Fused feature context in one agent call

## The two execution layers

Fused MCP `execute` runs TypeScript and can dynamically loop over API results. A single agent tool invocation can therefore resolve a feature, collect its evidence and flow nodes, and call a reusable symbol-context operation for every selected symbol.

The declarative `unified_operations.bindings` graph in fused-cli 0.17.1 remains fixed. Bindings declare `operation`, `input`, `depends_on`, `rollback` and `output`; they do not declare `for_each`. Use Unified Operations for the reusable fixed subgraphs, and use the MCP `execute` script for bounded dynamic fan-out.

## MCP configuration

`developa` and `v1` are placeholders for the imported service key and immutable service version.

```yaml
apiVersion: fused/v1
kind: mcp
name: developa-code-intelligence
version: "1.0.0"
bucket: default

services:
  developa:
    version: "v1"
    operations:
      - resolveRepository
      - project
      - capabilities
      - features
      - feature
      - codeFlow
      - symbol
      - symbolSource
      - calls
      - implementations

unified_operations:
  code.repository_for_path:
    description: >-
      Resolve an exact engine-visible repository root to its stable repository
      identity without exposing the root.
    input:
      type: object
      additionalProperties: false
      required: [project_path]
      properties:
        project_path:
          type: string
          minLength: 1
          maxLength: 4096
    bindings:
      repository:
        service: developa
        operation: resolveRepository
        input:
          path: "${input.project_path}"
    output:
      type: object
      additionalProperties: false
      required: [id, name]
      properties:
        id: "${response.repository.id}"
        name: "${response.repository.name}"

  code.feature_context_seed:
    description: >-
      Resolve one inferred feature against its authoritative saved snapshot and
      return matching feature summaries, analysis metadata and current retrieval
      capabilities. This operation does not run inference.
    input:
      type: object
      additionalProperties: false
      required: [repository_id, feature_query]
      properties:
        repository_id:
          type: string
          pattern: "^[a-f0-9]{64}$"
        snapshot_id:
          type: string
          pattern: "^[a-f0-9]{64}$"
        feature_query:
          type: string
          minLength: 1
          maxLength: 200
    bindings:
      project:
        service: developa
        operation: project
        input:
          repository: "${input.repository_id}"

      capabilities:
        service: developa
        operation: capabilities
        input:
          repository: "${input.repository_id}"

      feature_hint:
        service: developa
        operation: features
        depends_on: [project]
        input:
          repository: "${input.repository_id}"
          snapshot: "${input.snapshot_id ?? response.project.snapshot.id}"
          q: "${input.feature_query}"
          limit: 2
          offset: 0

      feature_match:
        service: developa
        operation: features
        depends_on: [project, feature_hint]
        input:
          repository: "${input.repository_id}"
          snapshot: "${response.feature_hint.saved_snapshot.id ?? input.snapshot_id ?? response.project.snapshot.id}"
          q: "${input.feature_query}"
          limit: 2
          offset: 0

    output:
      type: object
      additionalProperties: false
      required:
        - repository_id
        - latest_snapshot_id
        - evidence_snapshot_id
        - match_count
        - run
        - matches
        - capabilities
      properties:
        repository_id: "${input.repository_id}"
        latest_snapshot_id: "${response.project.snapshot.id}"
        evidence_snapshot_id: "${response.feature_hint.saved_snapshot.id ?? input.snapshot_id ?? response.project.snapshot.id}"
        match_count: "${response.feature_match.total}"
        run:
          type: object
          value: "${response.feature_match.run}"
          additionalProperties: true
        matches:
          type: array
          value: "${response.feature_match.items}"
          items:
            type: object
            additionalProperties: true
        capabilities:
          type: object
          value: "${response.capabilities}"
          additionalProperties: true

  code.feature_context_graph:
    description: >-
      Read one exact feature and its bounded static feature flow from an
      authoritative repository snapshot without running inference.
    input:
      type: object
      additionalProperties: false
      required: [repository_id, snapshot_id, feature_id]
      properties:
        repository_id:
          type: string
          pattern: "^[a-f0-9]{64}$"
        snapshot_id:
          type: string
          pattern: "^[a-f0-9]{64}$"
        feature_id:
          type: string
          pattern: "^[a-f0-9]{64}$"
        depth:
          type: integer
          minimum: 1
          maximum: 12
          default: 8
        node_limit:
          type: integer
          minimum: 1
          maximum: 100
          default: 80
    bindings:
      feature:
        service: developa
        operation: feature
        input:
          repository: "${input.repository_id}"
          snapshot: "${input.snapshot_id}"
          feature: "${input.feature_id}"
      flow:
        service: developa
        operation: codeFlow
        input:
          repository: "${input.repository_id}"
          snapshot: "${input.snapshot_id}"
          feature_id: "${input.feature_id}"
          depth: "${input.depth?}"
          limit: "${input.node_limit?}"
    output:
      type: object
      additionalProperties: false
      required: [feature, flow]
      properties:
        feature:
          type: object
          value: "${response.feature}"
          additionalProperties: true
        flow:
          type: object
          value: "${response.flow}"
          additionalProperties: true

  code.symbol_context:
    description: >-
      Return one snapshot-pinned symbol, its retained body, bounded incoming and
      outgoing calls, and static interface implementation candidates.
    input:
      type: object
      additionalProperties: false
      required: [repository_id, snapshot_id, symbol_id]
      properties:
        repository_id:
          type: string
          pattern: "^[a-f0-9]{64}$"
        snapshot_id:
          type: string
          pattern: "^[a-f0-9]{64}$"
        symbol_id:
          type: string
          pattern: "^[a-f0-9]{64}$"
        source_limit:
          type: integer
          minimum: 1024
          maximum: 16384
          default: 8192
        call_limit:
          type: integer
          minimum: 1
          maximum: 25
          default: 10
    bindings:
      symbol:
        service: developa
        operation: symbol
        input:
          repository: "${input.repository_id}"
          snapshot: "${input.snapshot_id}"
          symbol: "${input.symbol_id}"

      source:
        service: developa
        operation: symbolSource
        input:
          repository: "${input.repository_id}"
          snapshot: "${input.snapshot_id}"
          symbol: "${input.symbol_id}"
          offset: 0
          limit: "${input.source_limit?}"

      incoming:
        service: developa
        operation: calls
        input:
          repository: "${input.repository_id}"
          snapshot: "${input.snapshot_id}"
          symbol_id: "${input.symbol_id}"
          direction: in
          limit: "${input.call_limit?}"
          offset: 0

      outgoing:
        service: developa
        operation: calls
        input:
          repository: "${input.repository_id}"
          snapshot: "${input.snapshot_id}"
          symbol_id: "${input.symbol_id}"
          direction: out
          limit: "${input.call_limit?}"
          offset: 0

      implementations:
        service: developa
        operation: implementations
        input:
          repository: "${input.repository_id}"
          snapshot: "${input.snapshot_id}"
          symbol: "${input.symbol_id}"
          limit: 20
          offset: 0

    output:
      type: object
      additionalProperties: false
      required: [symbol, source, incoming, outgoing, implementations]
      properties:
        symbol:
          type: object
          value: "${response.symbol}"
          additionalProperties: true
        source:
          type: object
          value: "${response.source}"
          additionalProperties: true
        incoming:
          type: object
          value: "${response.incoming}"
          additionalProperties: true
        outgoing:
          type: object
          value: "${response.outgoing}"
          additionalProperties: true
        implementations:
          type: object
          value: "${response.implementations}"
          additionalProperties: true
```

No rollback is declared because every binding is read-only.

## One MCP `execute` call

The agent sends one `execute` invocation containing a bounded TypeScript program. It first resolves an exact project path when the caller did not supply a repository ID, then resolves the feature and its authoritative saved snapshot. The loop dynamically expands the feature evidence and flow nodes through `code.symbol_context`.

```typescript
if (Boolean(input.repository_id) === Boolean(input.project_path)) {
  throw new Error("provide exactly one of repository_id or project_path");
}

const scope = input.repository_id
  ? { id: input.repository_id }
  : await call("code.repository_for_path", {
      input: { project_path: input.project_path },
      targets: ["repository"],
    });

const seed = await call("code.feature_context_seed", {
  input: {
    repository_id: scope.id,
    snapshot_id: input.snapshot_id,
    feature_query: input.feature_query,
  },
  targets: [
    "project",
    "capabilities",
    "feature_hint",
    "feature_match",
  ],
});

if (seed.match_count !== 1) {
  throw new Error(`feature_query resolved to ${seed.match_count} features`);
}

const graph = await call("code.feature_context_graph", {
  input: {
    repository_id: seed.repository_id,
    snapshot_id: seed.evidence_snapshot_id,
    feature_id: seed.matches[0].id,
    depth: input.depth ?? 8,
    node_limit: input.node_limit ?? 80,
  },
  targets: ["feature", "flow"],
});

const orderedIds = [
  ...graph.feature.evidence.map((item) => item.symbol_id),
  ...graph.flow.seed_ids,
  ...graph.flow.nodes.map((node) => node.symbol.id),
];

const uniqueIds = [...new Set(orderedIds)];
const maxSymbols = Math.min(input.max_symbols ?? 16, 24);
const selectedIds = uniqueIds.slice(0, maxSymbols);
const symbols = [];

// Sequential Unified calls keep fan-out bounded. Each symbol operation still
// runs its independent physical reads concurrently inside Engine.
for (const symbolId of selectedIds) {
  symbols.push(await call("code.symbol_context", {
    input: {
      repository_id: seed.repository_id,
      snapshot_id: seed.evidence_snapshot_id,
      symbol_id: symbolId,
      source_limit: input.source_limit ?? 8192,
      call_limit: input.call_limit ?? 10,
    },
    targets: ["symbol", "source", "incoming", "outgoing", "implementations"],
  }));
}

return {
  ...seed,
  ...graph,
  symbols,
  completeness: {
    selected_symbols: selectedIds.length,
    available_symbols: uniqueIds.length,
    truncated: selectedIds.length < uniqueIds.length || graph.flow.truncated,
    omitted_symbol_ids: uniqueIds.slice(maxSymbols),
    limitations: graph.flow.limitations,
  },
};
```

This is one agent-facing MCP tool invocation, but it deliberately produces several authorized Engine operations and receipts internally. Provider credentials remain inside the Fused bucket.

## Result-size boundary

Dynamic looping does not remove the MCP 1 MiB execution-result limit. Cap symbols, call pages and source chunks; return explicit omitted IDs and truncation state. A typical feature should fit in one invocation. The agent can make a deliberate continuation call for a very large feature rather than receiving silently incomplete context.

If the desired interface is instead one literal `call("code.feature_context")` with no TypeScript orchestration, then a server-side Developa `featureContext` physical endpoint is still required because the declarative Unified binding graph itself has no loop field in fused-cli 0.17.1.
