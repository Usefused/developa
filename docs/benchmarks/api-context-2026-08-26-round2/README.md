# Blind API context experiment — preloaded contract

## Result

The fresh agent made **zero OpenAPI/spec discovery requests**. Preloading a 3,768-byte generic API briefing removed the rediscovery overhead for this trial. Total transferred response bodies fell from at least **1,056,093 bytes to 266,103 bytes**, a reduction of approximately 74.8% against the previous known total.

The agent recovered all 15 declarations and the file's principal behavior, contracts, dependencies, persistence boundaries and test evidence. It scored **23.5/24 on the unchanged checklist**, compared with 24/24 previously. It also made **one incorrect inference about supporting code**, outside that checklist: it assumed a missing Content-Type header prevents JSON decoding. The API contained the helper needed to disprove this, but the agent did not retrieve it.

This is a successful removal of discovery work, not proof of perfect context recovery or a production client-cache implementation. No product code or API behavior changed.

## Controls

- Same target: `internal/application/workspace_folders.go`, 168 lines / 4,855 bytes.
- Same repository and pinned snapshot as [trial 1](../api-context-2026-08-26/README.md); exact identifiers and hashes are in [method.json](method.json).
- New agent with `fork_turns=none`. It received no original report, source explanation, checklist, score, known omissions, or target-specific helper names.
- The generic [API briefing](api-reference.md) was supplied directly in its task. Routes and parameter bounds were checked against the existing spec; downloading or rediscovering the spec was forbidden.
- Only GET requests to the supplied local snapshot API were allowed. Direct source/filesystem/docs/Git/database reads, UI inspection, tests, builds, mutations and inference were forbidden.
- Full captured implementations in API responses were allowed. This was not a summaries-only test.
- No artificial request cap. The agent chose its own retrieval sequence and report depth.
- Only operational progress communication was sent. The first independent report was preserved without correction or retry.

The parent verified the 15 target excerpts against local source before dispatch and verified unchanged source, original rubric and briefing hashes afterward. Source access restrictions were instructions, not an OS sandbox removing the checkout. The request ledger and compliance statement are evidence of the recorded behavior, not a complete independent tool audit.

## Source comparison

| Area | Finding |
| --- | --- |
| Purpose and inventory | All 15 declarations, including all 10 named functions/methods, correctly identified. |
| Validation and filesystem access | Path/name byte limits, rooted browsing, canonical-path registration, error sanitization and race limitations recovered. |
| Pagination and cancellation | Correct raw-entry offset semantics, 100-entry chunks, empty and exact-multiple pages, cancellation and maximum-offset continuation issue. |
| Page filtering | Correct `IsDir() && name != ".git"` predicate, but the report did not explicitly recover the directory-symlink-entry exclusion. Half credit for B09. |
| Registration and persistence | Correct Git-root validation, copied defaults, reuse, 32-workspace cap, persistence before publication/start, and delegated audit. |
| Relationships | Correctly distinguished resolved local calls from inferred HTTP interface wiring; did not treat empty incoming edges as unused code. |
| Tests | Required target test evidence recovered without claiming execution; additional HTTP test criticism was incorrect as described below. |

The [scorecard](scorecard.json) retains all original criteria verbatim. A checklist score is coverage of those criteria, not a general accuracy percentage. We did not add a new grading criterion after seeing the mistake.

The information supplied was also different: trial 1 could read behavioral descriptions in the full OpenAPI document and cited one for symlink-entry filtering. Trial 2 received only generic inspection routes and response conventions. This is a practical preloaded-contract rerun, not an experiment that isolates caching while holding all available documentation identical.

### Verified incorrect inference

The report's transport section says registration requires a JSON content type. Its test section then says the invalid-body tests can pass because they omit Content-Type and are rejected before decoding.

Both statements rest on the same wrong assumption. `jsonContentType` in `internal/transport/http/scan.go:47` explicitly returns `true` for an empty header. The body decoder is therefore reached in those tests; missing Content-Type is not the alleged reason they reject the input.

The parent fetched the complete helper through the **same pinned API** after the independent report. That one response was 1,175 bytes; it is recorded separately in [parent-audit.json](parent-audit.json), excluded from agent metrics. The helper matched local source. The original agent ledger contains no lookup for that symbol or file.

This is an agent retrieval/reasoning mistake, not missing parser data. Trial 1 left the same point uncertain; trial 2 turned an unverified assumption into a definite criticism. Neither report was corrected by the agent after feedback. The originals remain available for inspection.

## Retrieval comparison

All sizes below are raw HTTP response-body bytes, not model tokens. KB/MB summaries use decimal units.

| Measurement | Trial 1 | Trial 2, briefing supplied |
| --- | ---: | ---: |
| HTTP attempts | 26 | 31 |
| Confirmed successful responses | 24 | 30 |
| OpenAPI discovery attempts | 4, including 2 confirmed successes | 0 |
| Known OpenAPI response bytes | At least 788,494 | 0 |
| Total response bytes | At least 1,056,093 | 266,103 |
| Successful context requests, excluding discovery | 22 | 30 |
| Context response bytes, excluding discovery | 267,599 | 266,103 |
| Snapshot details/diagnostics bytes | 93,473 | 93,473 |
| Other source/metadata/relationship bytes | 174,126 | 172,630 |
| Target metadata plus complete symbol page | 25,274 | 25,273 |
| Checklist coverage | 24/24 | 23.5/24 |
| Ollama inference requests | 0 | 0 |

The improvement is almost entirely removal of discovery: **context-only bytes fell by just 1,496 bytes (0.56%)**, with eight more successful requests. More smaller requests are not inherently a problem, but this run does not establish better context-query efficiency. The supplied 3,768-byte briefing also consumes prompt context; it is not free onboarding or an HTTP response.

Trial 1 had one failed no-response attempt and one discovery attempt whose outcome/size/time was lost. Trial 2 recorded one sandbox-blocked attempt, then a successful approved retry; all 31 ledger entries retain their outcomes and measurements. The ledger totals were independently recomputed.

Trial 2 reports 326.345 seconds (about 5.4 minutes) through artifact-payload preparation, excluding final artifact writing/rendering. Trial 1's approximately 6.6 minutes was measured from dispatch to artifact creation. These endpoints and the agents' retrieval choices differ, so this is not a controlled speed comparison. Summed HTTP durations were 256.20 ms for trial 1's retained requests and 347.136 ms for trial 2; those exclude reasoning/tool scheduling/approval time and are not end-to-end latency.

Trial 2 reported approximately 97,772 inspected projection characters and 65 distinct declarations totaling 26,638 source characters. Trial 1 counted declaration instances including duplicates, so those declaration/source totals are not directly comparable. Exact model token usage is unknown in both trials.

## Implications

1. Give agents a compact API contract once, preserving repository/snapshot scope. This trial demonstrates that spec rediscovery is unnecessary for subsequent source retrieval; no server caching change was needed for the experiment.
2. Keep reports evidence-based: fetch a helper before asserting what its guard does, or state uncertainty. The missing lookup already exists in the API and does not require another LLM feature.
3. Consider a compact status/limitations endpoint and filtered/paged diagnostics. The same 93,473-byte repository-wide details response was used in both trials, approximately 35.1% of trial 2's transfer.
4. Test more files and enforce isolation by removing checkout access before drawing general conclusions. This was one small, fully captured Go file, with no generated explanations required.

## Evidence and verification

- [First independent report](agent-report.md), copied unchanged, including its incorrect claim.
- [Request ledger](requests.json), copied unchanged.
- [Original frozen baseline](../api-context-2026-08-26/baseline.json).
- [Per-item scorecard](scorecard.json) and [computed comparison](comparison.json).
- [Supplied generic API briefing](api-reference.md) and [trial controls](method.json).
- [Parent's supporting-helper verification](parent-audit.json).

Artifact copies were checked byte-for-byte, JSON totals and snapshot-scoped GET routes were validated, and source/baseline/reference hashes remained unchanged. No product code changed, no inference was invoked, and no tests were run for this documentation-only experiment.
