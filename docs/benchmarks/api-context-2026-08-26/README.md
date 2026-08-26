# Blind API context experiment — 26 August 2026

## Result

The independent agent recovered all **24 items in the prewritten source-based checklist** for `internal/application/workspace_folders.go`. It correctly explained every declaration, filesystem confinement, raw-entry pagination, registration/error logic, telemetry, persistence boundaries, HTTP wiring and available tests. No substantive contradiction with the target implementation was found.

This is a successful file-context retrieval trial, not a claim of 100% system accuracy. The agent used full captured function implementations, not just summaries. There was one avoidable uncertainty about a related HTTP helper, and retrieval overhead was substantial.

## Setup and controls

- Target: 168 lines / 4,855 bytes, with 15 indexed declarations including 10 named functions/methods.
- Source SHA-256: `47f90e47f09358bc9a609a93196342f9382c417926bf03e53275cce0cdfea0a3`.
- Repository: `cd106ad658f4cd3b9b86829ca9fef7f62d8be9fb75e0f366755d66aafa197732`.
- Snapshot: `70201cf592374b624f0ec7609643cc5979a7aa3d81534a277f4349c96408af82`.
- Parent read the source and supporting files, then froze [24 criteria](baseline.json) before dispatching the subagent. Baseline file SHA-256: `a039f1f864a0b4f1bf757ae03c16896840af94f17720a8666f19eb077ab89f70`.
- The subagent started with `fork_turns=none`: no inherited conversation or source explanation. It received only the file path, API origin, repository/snapshot IDs, local access credential, generic reporting requirements and experiment restrictions.
- Only GET requests to the local API were allowed. Direct workspace/docs/test/Git/database reads, mutations, inference, test execution and consultation with other agents were forbidden. Source excerpts inside API responses were allowed.
- Parent checked all 15 target excerpts plus 100 declarations in seven supporting files against the local source; they matched. No API implementation or source changes were made during the trial, and the target/baseline hashes remained unchanged.
- Progress messages supplied no semantic hints or grading feedback. This record preserves the agent's first independent report, without correction or a second attempt.

The restriction was procedural, not an OS sandbox that removed filesystem capability. The request ledger and agent's compliance statement support the API-only account; they are not a separately captured audit of every possible tool action. A stricter future trial should run an agent without the checkout mounted and enforce GET-only access through its available tools.

## Comparison

| Area | Source-based notes versus agent report |
| --- | --- |
| Purpose and declarations | Matched: service state, all named methods/helpers, three fields and deferred closure. |
| Validation and confinement | Matched: byte limits, NUL rejection, root membership, rooted browsing, canonical-path registration and distinct race limitations. |
| Pagination and cancellation | Matched: 100 raw entries before filtering, bounded skipping, empty/exact-multiple pages, error mapping and cancellation with an already-built page. |
| Registration and persistence | Matched: exact Git root, copied defaults/name trimming, duplicate reuse, 32-workspace limit, persistence before tracker admission/start, and delegated audit. |
| Call relationships | Matched, with appropriate qualification: concrete local calls are resolved; HTTP interface wiring is inferred from captured construction/assignment and callsites. |
| Tests and unknowns | Matched: retrieved unit/integration test evidence without claiming execution; distinguished complete source capture from partial static call analysis. |

The [scorecard](scorecard.json) records one point for each of the 24 frozen items. Coverage is scoped to those criteria, not every fact about the program or all adjacent helpers.

### A small remaining context gap

The agent left open whether tests without `Content-Type` could be rejected before reaching JSON validation. `jsonContentType` in `internal/transport/http/scan.go:47` explicitly accepts an empty header. The parent retrieved that helper through the same pinned API after the independent report; it was complete and available, with symbol ID `9e980fecba41df2e91da98959af23603fdc83777296f7883b6bc05c6db2ff5ec`.

This was an omitted lookup, not missing index data. The agent expressed uncertainty rather than falsely asserting that an absent header is rejected. It illustrates why a good report can match the main file checklist without recovering every detail in the parent's surrounding context.

The agent also correctly noticed an extra edge case: a full page at offset 100000 emits next_offset 100100, which the service then rejects. That observation follows from the captured source; no runtime reproduction was performed in this trial.

## Retrieval cost

| Measurement | Result |
| --- | ---: |
| GET attempts | 26 |
| Confirmed HTTP 200 responses | 24 |
| Known no-response attempts | 1 |
| Attempts with lost outcome/size/time metrics | 1 |
| Measured response-body bytes | At least 1,056,093 (1.06 MB decimal) |
| Retained request/read durations, summed | 256.20 ms; not end-to-end latency |
| Approximate time from dispatch to report artifact | 6.6 minutes, including agent/tool/reasoning work |
| Source/context requests pinned to the snapshot | 22 |
| Reported inspected source paths | 13 |
| Reported inspected symbol excerpt instances | 99, including duplicates |
| Reported inspected source characters | About 33,185, including duplicates |
| Reported structured projection characters inspected | About 146,954, plus approximately 9,000 from clipped output |
| Ollama inference requests | 0 |

These are transfer and inspection measurements, **not model token counts**. One early OpenAPI request lost its metrics because the agent's wrapper tried to parse output before retaining a truncation warning. The agent disclosed this; no values were invented. The first loopback attempt returned no response and later escalated requests worked. This operational failure is not evidence of a server defect.

| Known payload category | Attempts | Bytes |
| --- | ---: | ---: |
| OpenAPI discovery | 4, two with confirmed bodies | 788,494 plus the unknown attempt |
| Snapshot diagnostics/details | 1 | 93,473 |
| Source, metadata and relationships | 21 | 174,126 |

Discovery plus diagnostics account for **83.5% of measured bytes**. The 394,247-byte OpenAPI document was downloaded twice with retained successful metrics; duplicate discovery was an agent/client inefficiency as well as a response-size problem. The target's own metadata and complete symbols needed only two GETs and 25,274 bytes. Comparing all 1.06 MB with the 4,855-byte target alone would be unfair: the investigation also retrieved related files and tests.

## What this says about the product

1. **The stored index works as a source-context interface.** Exact implementations, signatures, positions, IDs and deterministic call evidence were enough for a fresh agent to reconstruct the file and its surrounding responsibilities without local file access or generated explanations.
2. **Graph-only navigation is insufficient for interface dispatch.** Parent checks found zero resolved incoming calls for the concrete `AddWorkspace` and `Folders` methods. The agent recovered their HTTP wiring through source retrieval instead. Empty caller lists must not imply unused code; interface/candidate relationships should remain separate from proven bindings.
3. **Cold discovery and broad responses cost too much.** Reduce duplicated OpenAPI components and cache discovery in the client. Offer compact discovery and projected symbol records, then fetch implementations when needed. These are proposed improvements, not existing endpoints.
4. **Diagnostics need narrower retrieval.** A status/limitations summary and paged or file-filtered diagnostics would avoid fetching repository-wide detail for one file. Small related requests remain appropriate; this does not require a single oversized context endpoint.
5. **Agent retrieval discipline matters.** Preserve request metadata before rendering capped output, keep parsed responses in memory, and inspect a named helper before raising a test-guard concern. The Content-Type question required another existing API lookup, not a new AI feature.

This file had complete excerpts, no saved AI reviews and only one function with a nonempty source-comment summary. The trial therefore does not establish that summaries alone provide equivalent context, or that the same result holds for very large/truncated functions, external dependencies, other languages, unresolved runtime behavior, or multiple independent agents.

## Preserved evidence

- [Independent agent report](agent-report.md), copied without modification.
- [Request ledger](requests.json), including null/unknown metrics.
- [Frozen parent baseline](baseline.json).
- [Per-item comparison](scorecard.json).

Only these benchmark documents were added after the run. No product code or API behavior was changed as part of this experiment.
