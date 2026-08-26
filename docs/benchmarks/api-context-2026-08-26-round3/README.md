# Blind context validation after navigation changes

## Concrete result

A fresh agent reconstructed the target file and its surrounding production behavior without reading the checkout. It recovered all 15 target declarations, inspected every target executable body through the pinned snapshot service, and matched all 24 items in the original frozen source checklist.

The material improvement was outside the target file. The agent independently followed the unresolved `WorkspaceStore.SaveWorkspaces` call through its interface-method reference, treated the returned PostgreSQL receiver as a conditional candidate, verified production construction/assignment separately, and then inspected the concrete transaction helpers. It correctly reported:

- transaction begin, rollback and commit;
- a transaction-scoped advisory capacity lock;
- a set-based workspace upsert;
- completed audit events and outbox rows in the same transaction;
- the earlier repository upsert outside that transaction, which may remain if later workspace persistence fails;
- manager publication and background scanning only after durable registration.

The agent also recovered the folder-symlink listing behavior, physical source positions, HTTP receiver wiring, authentication, error mapping, telemetry, configuration bounds and relevant test source. It did not claim that conditional interface candidates were resolved runtime edges.

This is substantially closer to the parent's source understanding than the previous runs. The new implementation solved the concrete persistence-discovery gap.

## Remaining differences

The report did not inspect `jsonContentType`, so it failed to record that an absent `Content-Type` header is accepted. It correctly described JSON decoding and did not repeat trial 2's false claim that the invalid-body tests only reach an earlier content-type rejection.

The report also introduced minor citation-path typos: it wrote `internal/persistence/postgres/...` instead of the API-returned `internal/store/postgres/...`, and once used singular `workspace_persistence.go` instead of `workspaces_persistence.go`. The symbol IDs, spans and implementation descriptions still referred to the intended declarations. These are agent rendering errors, not missing source metadata.

For transparency, the unchanged original rubric scores 24/24. Five additional acceptance items cover navigable calls, interface candidates/wiring, concrete persistence, body/location coverage and supporting-guard verification; the result is 4.5/5 because the reusable content-type helper was missed. The [result](result.json) records the individual assessments. Checklist coverage is not a general accuracy percentage.

## Retrieval behavior

The agent made 89 tracked GET attempts: 87 succeeded, one initial sandbox-level attempt received no HTTP response, and one intentional missing-test-path lookup returned 404. Responses totaled 490,891 bytes. It made no OpenAPI or model-inference requests.

This run prioritized thorough understanding rather than request efficiency. The agent inspected configuration, transport, persistence, telemetry, manager lifecycle and test evidence well beyond the target body. An MCP client should expose the same small tools and generic traversal rules while letting the agent stop at the depth required by its task.

## Evidence

- [First independent report](agent-report.md), copied unchanged.
- [Complete request ledger](requests.json), copied unchanged.
- [Assessment and metrics](result.json).
- [Artifact hashes](manifest.json).

The test's checkout restriction was procedural rather than enforced by removing filesystem capability. The ledger and agent statement support compliance but are not an independent audit of all possible tool activity.
