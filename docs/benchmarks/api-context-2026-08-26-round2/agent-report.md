# Blind file-understanding report — API contract preloaded

## Scope and confidence

- Target: `internal/application/workspace_folders.go`
- Repository: `cd106ad658f4cd3b9b86829ca9fef7f62d8be9fb75e0f366755d66aafa197732`
- Snapshot: `70201cf592374b624f0ec7609643cc5979a7aa3d81534a277f4349c96408af82`
- Read-only API origin: `http://127.0.0.1:18089`

This is my first independent report, based solely on GET responses from the supplied snapshot API. No tests, builds, repository code, or Git commands were executed.

**Purpose:** this application service exposes configured filesystem roots, lists child directories within those roots, and registers an already existing Git working-tree root as a managed workspace. It connects filesystem selection and validation to workspace persistence and manager lifecycle. It is not itself the HTTP transport, database implementation, or source-analysis engine.

Confidence is high about the target's captured implementation and its immediate contracts. File metadata reports completeness=complete and 15 declarations; every inspected declaration had source_truncated=false. Snapshot metadata separately reports source_complete=true, syntax analysis, 220 files and 3,167 symbols. Call analysis is **partial**, with 1,347 resolved and 9,730 unresolved calls across the snapshot. Those are repository-wide counts, not counts for this file. Snapshot metadata says dirty=true, branch=main and commit=""; a commit identity cannot be established.

References T1–T15 below identify declarations in the target file, with physical line ranges and full API symbol IDs. S references identify supporting declarations with file:line and full symbol IDs in the evidence catalog.

## 1. Declaration inventory

All T references are in internal/application/workspace_folders.go. End positions come from API spans, whose ends are exclusive; line ranges identify the captured declaration.

| Ref | Declaration | Lines | API symbol ID |
|---|---|---:|---|
| T1 | struct WorkspaceService | 17–21 | e3a13c4b6be7e66e1e05d939a9f421e24a9a665d3fbe4c780019929d78951d3a |
| T2 | field group | 18–18 | 1e9e8dee366af2352a447400491e75207289f3aca659fb3948fcd34d3df9424e |
| T3 | field defaults | 19–19 | 632175354a4afd46250d694e27e91f91b285c78ad37eca6e048dd74b6823d84a |
| T4 | field roots | 20–20 | c5038c1abb2c1cbc69d8ccc89f4df5b2119c276e2b891092d63424fa0cbf9496 |
| T5 | function NewWorkspaceService | 23–38 | 4759f37da65a596ae90b4d7ee9fbad884cca2883fe6390399d43132d68aa2c10 |
| T6 | method FolderRoots | 40–42 | a655cf27acc9e43428f64b8ee1863f9dc3b0cf3e1cda210b326d9c3672b5cbee |
| T7 | method root | 44–51 | 628e66c17df805e614df8055067747b19416d478b30ad51aaef8f2a5fd264a6f |
| T8 | function validFolderPath | 53–55 | 014e944000162b649d76e506b53e3a28954be0648f11999eb691d9a04d22aa3c |
| T9 | method Folders | 57–77 | 17ca466ce0140fc95ae155623497e5c11b00de519977364c3b6ffdd675d2b215 |
| T10 | function readFolderPage | 79–98 | 349451725d31a5f46fa0b4e0030571e667fa305a7bee8b4512c3a95d564560d6 |
| T11 | function skipFolderEntries | 100–115 | ae71d29d1b81583741a5a31b0ebdc41c762a635bdd6ef111cb598aaf7bac771a |
| T12 | method AddWorkspace | 117–142 | 7d64cc3388edf101b5ec47c97afd6be0aad33ced6371de00d4f00cff67db01ca |
| T13 | closure $closure1 | 121–127 | bba44b7a469a31bf6b538f13c85d5794ed988b2e59b81f4779cd95c5352df730 |
| T14 | function gitRegistrationError | 144–149 | 107dfd65166cb43b5a5618204e8c600b28999cb887d80086549f584f9491c50d |
| T15 | method selectedPath | 151–168 | bf8d3055a2bade2f934c3195443d5fd5a0bcb3ac6c247bc0958a62eb23f9b05c |

The inventory is one struct, three fields, five functions, five methods, and one closure. The closure is nested in AddWorkspace; it is not another top-level entry point.

WorkspaceService holds a workspace group, default manager configuration, and configured folder-root records. Its public operations are NewWorkspaceService, FolderRoots, Folders, and AddWorkspace. The other routines enforce selection, read, pagination, and error behavior. [T1–T15]

## 2. Public contracts and root construction

### NewWorkspaceService(group, defaults, paths) → (*WorkspaceService, error)

For each configured path, construction calls canonicalWorkspacePath, verifies that os.OpenRoot succeeds, closes the temporary handle, and appends a FolderRoot. The root ID is the hexadecimal SHA-256 of the canonical path string; Name is filepath.Base(root), and Path is the canonical absolute path. Input order is preserved. No deduplication or root-count cap appears here. Empty input produces an empty configured-root list. [target:23–38, T5]

canonicalWorkspacePath rejects an empty path, obtains filepath.Abs, and resolves symlinks. It returns generic configuration errors for unavailable paths. NewWorkspaceService passes those errors through, but maps an os.OpenRoot failure to ErrFolderForbidden. Root handles are not retained after construction. [S1; target:26–35, T5]

The constructor does not validate a nil group. A nil group is intentionally usable for browsing-only fixtures, but a successful registration requires a usable group. The captured constructor supplies no protection against a nil group reaching AddWorkspace's group.Add call. [T5, T12; folderFixture at internal/application/workspace_folders_test.go:14]

### FolderRoots(context.Context) → ([]FolderRoot, error)

Returns a newly allocated slice containing the root values and nil error. Callers cannot mutate the service's slice through the returned slice. The context is unused, so cancellation does not affect this operation. FolderRoot exposes id, name, and path; the path is not redacted in this domain result. A root ID is a lookup key, not evidence of caller authorization. [target:40–42, T6; S2]

### Folders(ctx, id, path, offset) → (FolderPage, error)

The root ID must match a configured root. The path must be nonempty, no more than 4,096 bytes, contain no NUL, and satisfy filepath.IsLocal. Use "." to select the configured root itself. Offset must be between 0 and 100,000 inclusive. Invalid path/offset is checked before root lookup and returns ErrInvalidInput; an unknown root returns ErrFolderForbidden. [target:44–65, T7–T9]

FolderPage has root_id, normalized path, items, and nullable next_offset. Each Folder has name and path. Page paths and child paths are converted to slash form; the page path is cleaned. The helper initializes items as an empty slice rather than nil. [target:79–98, T10; S3–S4]

### AddWorkspace(ctx, request) → (AddedWorkspace, error)

The request carries root_id, path, and name. Selection applies the same relative-path rules, and rejects names longer than 200 bytes or containing NUL. Name validation precedes strings.TrimSpace; empty/whitespace names are not rejected here. RootID has no independent format/length validation in this method; exact configured-root lookup is the boundary. [target:151–168, T15; S5]

On success the result embeds the manager's Repository and an already_added boolean. Defaults are copied and only RepositoryPath and RepositoryName are overwritten; PollInterval and ScanTimeout remain inherited. ManagerConfig contains those four fields. [target:135–141, T12; S6, S8, S15]

## 3. Decision logic and bounds

### Browsing is handle-confined and paginated by raw directory entries

Folders opens os.Root for the configured root, then opens the requested path through that root. Its comment explicitly explains that root confinement includes symlink components. Both handles are closed with deferred calls; close errors are ignored. Root-open, path-open, and non-EOF directory-read failures are collapsed to ErrFolderForbidden, rather than exposing OS errors. [target:65–76, T9; target:84–87, T10]

skipFolderEntries reads at most 100 raw entries per iteration until it has skipped offset entries or encounters EOF. It checks ctx.Err before each skipping chunk. readFolderPage then reads up to 100 raw entries and includes only entries for which IsDir is true and Name is not ".git". [target:79–115, T10–T11]

Consequences visible in the implementation:

- Offset counts **all directory entries**, not returned folders. A nonfinal page can therefore contain fewer than 100 folders, or no folders at all.
- next_offset is offset+100 whenever exactly 100 raw entries were read. It does not look ahead, so an exact final batch can advertise one additional empty page.
- The code performs no sort. These are live directory reads on newly opened handles, not a stable snapshot/cursor across requests; concurrent directory changes are not reconciled here.
- Pagination bounds per-read allocation, but later pages rescan the skipped prefix. An accepted offset of 100,000 can require 1,000 skipping reads plus the page read.
- A full page at offset 100,000 yields next_offset=100,100, which the public method would reject. This continuation-at-the-cap behavior is visible in source and was not exercised by the retrieved tests.
- ".git" is hidden from returned child entries, but the input-path validator does not prohibit explicitly requesting ".git" or a path beneath it. Hiding its listing entry is not a general metadata-access denial rule.
- An offset beyond EOF returns an empty page with no continuation, subject to the final context check.

Cancellation is cooperative. With offset>0 it is checked during skipping; with offset=0 it is returned at the end of readFolderPage. Folders performs root lookup and filesystem opens before that check. A context error can accompany an already populated page at the service level. [T9–T11]

### Registration validates a resolved path, then delegates publication

selectedPath resolves symlinks in Join(root.Path, request.Path), computes its path relative to the configured root, and requires the result to remain filepath.IsLocal. Resolution/relative-path failures and containment failures return ErrFolderForbidden. Unlike browsing's os.Root handle, this path is subsequently passed as a string. [target:151–168, T15]

AddWorkspace next calls source.Open(ctx, path, source.Options{}). The captured Git implementation canonicalizes the root, runs Git rev-parse --show-toplevel, resolves the discovered top level, and requires equality with the selected root. Thus choosing an ordinary subdirectory inside a Git working tree does not satisfy the exact-root contract. It also rejects a root string ending in "/*", to avoid Git trust-wildcard interpretation. [target:132–134, T12; S9]

If source.Open fails, gitRegistrationError returns the current context error if present; otherwise it returns ErrNotGitRepository. Other Git discovery failure details are deliberately lost at this boundary. Successful discovery is followed by group.Add; its errors pass through unchanged. [target:132–149, T12, T14]

Workspaces.Add holds the group mutex, rejects a closed group, then checks for an existing manager by exact configured path before enforcing the 32-manager cap. Existing managers return reused=true without applying a new name or creating a new registration. The preflight source.Open still occurred even for these duplicates. New additions require the store to implement WorkspaceStore, create a manager, persist its registration, publish it in memory, establish the fallback if it is the first, and start it if the group is already running. On persistence failure the new manager is closed before the error returns. [S10–S11]

persistAddedWorkspace rechecks that manager.source is nonnil and its discovered Root equals the expected selected path before calling SaveWorkspaces. Its comment identifies a selection/discovery path-change race as the reason. This is useful revalidation, but the excerpts and tests do not establish atomic filesystem identity across all steps or prove comprehensive race safety. [S12]

## 4. Errors, side effects, and execution identity

| Condition | Service-level outcome |
|---|---|
| Empty/nonlocal/NUL/overlong selected path; invalid offset; overlong/NUL name | ErrInvalidInput |
| Unknown root; filesystem access/containment failure | ErrFolderForbidden |
| Non-Git or otherwise failed Git preflight with active context | ErrNotGitRepository |
| Canceled/deadline context during relevant browse or Git checks | Context error |
| Closed workspace group | context.Canceled from Workspaces.Add |
| New workspace beyond 32 managers | ErrWorkspaceLimit |
| Store lacks WorkspaceStore | ErrNotConfigured |
| Manager creation or registration persistence fails | Delegated error |

Construction/browsing read the filesystem and open/close handles. No write or Git initialization is visible in their captured implementations. The retrieved non-Git test explicitly checks that registration did not create ".git"; this is test intent, not an executed observation. [T5, T9–T12; S26]

Successful registration can write through the WorkspaceStore boundary, update the manager group, and launch a manager goroutine. Manager.Start guards started/closed/unconfigured state, marks status="scanning", and calls go m.run. Therefore registration success does **not** mean scanning or analysis has completed. This experiment did not execute any such registration. [S10, S12–S14]

AddWorkspace starts a "workspace.add" span with the request context, adds execution.started, and passes the derived context into Git and workspace-group operations. Its deferred closure observes the named error result: errors call telemetry.Fail with a fixed "workspace_registration_failed" reason; success adds execution.completed. The closure runs before the deferred span.End. scanTracer uses the "developa/application" tracer. Fail sets error status and emits execution.failed with error.type. No path, name, source, or secret is attached by this target's span code. [target:117–127, T12–T13; S16–S17]

The persistence helper creates execution identity with actor "operator" and trigger "workspace.add" and passes it to SaveWorkspaces. The retrieved integration audit assertion expects one completed durable audit record with a nonempty trace ID. The concrete SQL persistence implementation was not inspected; this report establishes the application boundary and test assertion, not a verified transaction guarantee. The target alone shows no durable failure-audit path, and its browsing methods have no explicit tracing. Instrumentation elsewhere was not exhaustively examined. [S12, S34]

## 5. Callers, callees, and integration boundaries

### Statically resolved API edges

The /calls responses label the following edges resolved:

- managedExplorerServer → NewWorkspaceService, at cmd/server/workspace_runtime.go:31. [S18 → T5]
- NewWorkspaceService → canonicalWorkspacePath, at target:26. [T5 → S1]
- Folders → validFolderPath, root, readFolderPage, at target:58, 61, 76. [T9 → T8/T7/T10]
- AddWorkspace → scanTracer, its deferred closure, selectedPath, source.Open, gitRegistrationError, Workspaces.Add, and Manager.Repository, at target:118, 121, 128, 132, 133, 137, 141. [T12 → S16/T13/T15/S9/T14/S10/S15]
- The deferred closure → telemetry.Fail, at target:123. [T13 → S17]
- Workspaces.Add → existingPath, NewManager, persistAddedWorkspace, Manager.Close, and Manager.Start, at internal/application/workspaces_persistence.go:81, 91, 95, 96, 104. [S10]

These are static local bindings, not execution-order traces, argument-compatibility proof, or build verification. Other helper relationships apparent in source, such as readFolderPage → skipFolderEntries, were not separately checked with /calls and are presented as source reading rather than an independently inspected resolved edge.

### Transport wiring and unresolved interface dispatch

managedExplorerServer constructs the concrete service and passes it into httptransport.Config.WorkspaceManagement. The domain WorkspaceManagement interface has exactly FolderRoots, Folders, and AddWorkspace signatures matching this service. These excerpts support the conclusion that the server wires this service into the HTTP workspace routes. [S7, S18]

mountManagedWorkspaces attaches authentication to its /api route group and mounts:

- GET /api/workspace-roots → roots → WorkspaceManagement.FolderRoots.
- GET /api/workspace-folders → folders → WorkspaceManagement.Folders.
- POST /api/repositories → add → WorkspaceManagement.AddWorkspace.

These routes are descriptions of captured code; **none of them were invoked by this experiment**. All actual requests were to the pinned snapshot inspection routes. [S19–S22]

The direct incoming-call pages for the concrete Folders and AddWorkspace methods were empty. Outgoing calls from their HTTP handlers explicitly mark WorkspaceManagement.Folders and WorkspaceManagement.AddWorkspace **unresolved**, reason=interface_or_type_parameter_dispatch, with no target ID. Therefore the transport-to-concrete-service relationship is an inference from matching interface and construction/wiring source, not a resolved edge. Empty incoming pages do not imply unused code. [S7, S18, S21–S22]

The transport parses only single-valued root_id/path/offset query keys, defaults offset to zero, and leaves semantic folder bounds to the service. Registration requires the JSON content type, bounds the body at 8,192 bytes, rejects unknown fields and trailing JSON, and requires root_id/path. It also calls sameOrigin before reading the body. New registrations yield 201; reused ones yield 200. A post-registration Explorer initialization failure can still make the HTTP request return an error after the application has registered the workspace. [S21–S24]

writeWorkspaceError maps not-Git to 422/not_git_repository, forbidden-folder to 403/folder_forbidden, and the workspace limit to 409/workspace_limit. Other errors delegate to generic writeError, whose implementation was not inspected. The integration validation helper expects a traversal request to produce 400. [S25, S33]

The captured service imports context, crypto/sha256, fmt, io, os, filepath, strings, domain, source/git, and telemetry. Standard-library and OpenTelemetry calls commonly appear unresolved with callee_binding_unavailable_call_or_conversion, consistent with the API's disclosed external-import limitations. No MCP caller, model adapter, or AI request was established from the inspected excerpts; absence from this focused investigation does not prove repository-wide absence.

## 6. Retrieved test evidence — not executed

| Evidence | Assertions visible in captured test source |
|---|---|
| S26, workspace_folders_test.go:25 | Temporary roots; outward symlink, "../", "/etc", and missing-path rejection for browsing and registration; plain directory reports ErrNotGitRepository; no ".git" created. |
| S27–S28, workspace_folders_test.go:47 and :63 | Creates 105 child directories plus ".git"; follows next_offset; each returned page has at most 100 items; obtains all 105 and excludes ".git". |
| S29, workspace_folders_test.go:83 | Rejects offsets -1 and 100001, unknown root, and canceled browsing context. |
| S30, workspace_folders_test.go:100 | persistAddedWorkspace with an unconfigured Manager returns ErrFolderForbidden before durable registration. |
| S31–S32, workspaces_integration_test.go:44 and :67 | Harness accepts *postgres.Store, creates persistent workspaces and an httptest server, starts the group; scenario uses integrationStore and integrationGitRepository helpers; expects 201 first add, 200 duplicate with same ID, snapshots, default routing, restore, and later source update. |
| S33, workspaces_integration_test.go:144 | Expects 401 for unauthenticated management GETs; 422 for non-Git, 400 for traversal, 403 for unknown root; failed validation leaves repository catalog empty. |
| S34, workspaces_integration_test.go:161 | SQL assertion requires exactly one workspace.add audit event with actor=operator, nonempty trace_id, outcome=completed. |
| S36, workspaces_integration_test.go:98 | Renames a checkout away, restores, and expects saved snapshot/source context to remain readable while project status is error. |
| S35, workspaces_integration_test.go:170 | Invalid request-body cases and cross-origin registration should be rejected. |

The persistence scenario is written against PostgreSQL types and integrationStore/integrationGitRepository fixtures; those fixture implementations and environment gates were not retrieved, so availability/setup cannot be independently established here.

A test-quality limitation is visible in S35: the invalid-body loop does not set Content-Type, while workspaceBody rejects that before decoding. Thus those cases can pass because of content-type rejection, without demonstrating that each named JSON/body rule was reached. The separate cross-origin assertion directly checks a 403 response. This is a source-level observation, not a test result.

The retrieved tests do not establish coverage for duplicate configured roots, root-ID stability, path/name byte-length boundaries, nil-group successful registration, full batches at offset 100000, exact-multiple terminal pages, concurrent filesystem replacement, or failure-audit behavior. Some may be tested elsewhere; the investigation was deliberately focused.

## 7. API strengths and missing/difficult information

**What worked well**

- File metadata immediately established package/imports, complete source capture, and the exact declaration inventory.
- Symbol catalog entries already included implementation excerpts, parameter/result contracts, spans, source-truncation flags, and stable IDs. No raw-file endpoint or local source read was needed.
- Direct symbol lookup made following resolved call targets efficient.
- Callsite resolution and explicit reasons prevented misreading interface calls or external calls as missing functionality.
- Snapshot-pinned test sources supplied concrete intended behavior without executing repository code.
- /details clearly separated source completeness from partial call analysis and disclosed deterministic selection constraints.

**Limitations encountered**

- Catalog q is lexical: q=ManagerConfig with kind=struct also returned Manager and WorkspaceService. Exact names/paths had to be checked.
- Empty concrete incoming-call pages required following construction/interface/transport evidence manually. The API appropriately did not invent a concrete dispatch edge.
- Tests are excluded from deterministic typed selection, so their callsites do not supply resolved incoming edges even when source calls are clear.
- The API does not resolve standard-library/external declarations and does not establish builds, platform behavior, runtime order, or full interface dispatch.
- /details had no file filter and transferred 93,473 bytes for repository-wide data. I inspected a bounded in-memory projection: full scalar/dictionary structure with arrays sampled to eight entries and their total sizes retained. It reported 384 call-analysis diagnostics; only the first eight were inspected. Those first diagnostics concerned excluded test variants. Target-specific diagnostic absence was not established.
- The file overview has no explanatory package/file documentation; most understanding came from source. The single captured confinement comment was useful.
- Source snapshots make the code explanation reproducible, but do not freeze runtime filesystem contents for Folders or prove the checkout currently matches this captured dirty snapshot.
- No saved feature claims or function reviews were required or consulted. No inference/generation endpoint was invoked.

The call-analysis limitations also disclose fixed gc/amd64 sizes, no workspace/replacement build evaluation, no fetched dependencies, and no proof of argument compatibility even for resolved bindings. Local named declarations are not indexed; anonymous functions are indexed separately. These constraints bound confidence, particularly around portability and runtime guarantees.

## 8. Protocol and measurement

All HTTP attempts used GET to the exact supplied loopback origin and the exact repository/snapshot prefix. All query values were URL encoded. The first request was blocked by the sandbox; it is recorded with null HTTP status and zero received bytes. I requested proper escalation and retried successfully. There were no further failures.

No OpenAPI/spec request, unpinned/default-workspace request, mutation, scan, analysis admission, model inference, generation, external-source request, repository command, test, build, shell-profile read, direct source read, documentation read, database access, or UI inspection occurred. The only filesystem writes are the two final artifacts specified in the task. Responses and the request ledger were retained in in-memory tool state. No preexisting output was read. The credential is omitted from this report and ledger.

No known protocol violation occurred. /details contained skipped-file names, including documentation/spec names, but not their contents; those files were not requested or read.

- HTTP attempts: **31** (30 HTTP 200 responses; 1 sandbox-blocked attempt with no HTTP response).
- OpenAPI/spec/discovery requests: **0**.
- Total raw response-body bytes: **266,103 bytes** (approximately 259.9 KiB).
- Summed HTTP durations, including the failed attempt: **347.136 ms**.
- Overall wall time from initial experiment/request setup through final artifact payload preparation: **326.345 seconds**. Final local-write tool execution and final-message rendering are outside this endpoint.
- Approximate inspected JSON projection content: **97,772 characters**.
- Distinct inspected declarations: **65**; their captured source totals **26,638 characters** and **752 line occurrences** (overlap possible).
- Inspected declarations marked source_truncated: **0**.
- Exact model input/output tokens and uninspected-content comprehension: **unknown**. Transfer bytes are not token usage.

HTTP duration is measured inside the request process from immediately before constructing the request through receiving its body (or failure). It excludes process startup, tool scheduling, and escalation approval time. Parallel durations are summed, not treated as wall time. Raw response-body byte counts were captured before projection/rendering; they include uninspected response fields. The approximate inspected-character figures count rendered JSON projections/source excerpts, not model tokens; excerpts can overlap, especially nested closures. Exact model token usage is unknown.

All returned catalog/call pages fit within their requested limits, so no further pages were necessary. The one repeated route was the initial file metadata request retried after sandbox denial; both attempts are retained.

## Supporting evidence catalog

Each entry below was retrieved from the pinned API. File:line is a physical source position, not a statement about the current workspace checkout.

| Ref | Symbol | File:line | API symbol ID |
|---|---|---|---|
| S1 | canonicalWorkspacePath | internal/application/workspaces.go:53 | bc2d241955188c69ad44ae52f26ab8eb3e8039b28f3decacd2f54500062e7fbc |
| S2 | FolderRoot | internal/domain/workspaces.go:26 | e8cb90c5ddea6d0b32d7fef6453ddc38ca5bbc93f18adc3ac4a66770c9d278b1 |
| S3 | Folder | internal/domain/workspaces.go:32 | d97910b0b77aab6471371c34ec51c119e3fd4614b41f6cb2a6cb3d1b1f07cd46 |
| S4 | FolderPage | internal/domain/workspaces.go:37 | 0813bc3112c26e16c1efd6d14e0040e128c8c40a11fdbe470c9067a124549bf2 |
| S5 | AddWorkspaceRequest | internal/domain/workspaces.go:44 | 48881377582f34952f75abd5e6609a3b8f3fb4414734a8955d58954179b406df |
| S6 | AddedWorkspace | internal/domain/workspaces.go:50 | 39bdfda83acf4151888728bd8908cd512a67308925a73f95f750fbfe76a26560 |
| S7 | WorkspaceManagement | internal/domain/workspaces.go:55 | 4d0448f9cb4f7dd8a61b4aee6572552e41fd0df5f085631919dea9946f616e38 |
| S8 | ManagerConfig | internal/application/manager.go:23 | 83d47243513b50b01e11513e9ce4f392d8a3fd4adb366b3cbf7c2925a37baae3 |
| S9 | Open | internal/source/git/open.go:32 | 0dbb93e1e0d9c098c57c2fe33a3de6f2604dc27cdb134daecac4fb2e38505474 |
| S10 | Add | internal/application/workspaces_persistence.go:75 | ad0f0c7a658baf1a964c090eac9650c3022b437cd79d5c5f847ef982ff97da30 |
| S11 | existingPath | internal/application/workspaces_persistence.go:110 | aa0f9a2cd0caa90690c6e93905cf943430fddddaedec405b6783831ad82a9ee9 |
| S12 | persistAddedWorkspace | internal/application/workspaces_persistence.go:119 | fb8908be485d6deabd7f89abd896bbf9cd8ac8fe847695d5a14fe12599b9a01d |
| S13 | NewManager | internal/application/manager.go:58 | df0e730ea8fa82a4d30cdb587df88e9677c68cc9336a4e4857570650366631a2 |
| S14 | Start | internal/application/manager.go:123 | a45e29eebeaed5f81b9c46bc82ced530244c7eafaf8b540b6ff2aab94c233d05 |
| S15 | Repository | internal/application/manager.go:105 | d21032b09cbbb3e715c9598beade67bafc6e8380543b399068672142784303a3 |
| S16 | scanTracer | internal/application/scan.go:134 | b953c30ba1fc2a56801fd9f74f795f6039f03369c894f1cc5c96dcc04a7fec3c |
| S17 | Fail | internal/telemetry/telemetry.go:88 | 857c1013361eb9a469b964747084ceda3a89aaea591f64dc599648f33f5b1d39 |
| S18 | managedExplorerServer | cmd/server/workspace_runtime.go:29 | fa73dccf6c4347f85093992b0cc2ef75fa74c32605e1cbaeaaa24896943eefe5 |
| S19 | mountManagedWorkspaces | internal/transport/http/workspaces.go:22 | 04a3e9ebbecde5264ff8caa15d7d768682ab69a16db63480ee987e68d866cdc3 |
| S20 | roots | internal/transport/http/workspaces.go:65 | e9eb667f9576ba154e6f0e31af67c9cd393c8d66b218c11dd6b087189944a62a |
| S21 | folders | internal/transport/http/workspaces.go:70 | e50875ac6c5bdec77ac88a5ca3cadc8e98552675b13b764a40d07267f25fb927 |
| S22 | add | internal/transport/http/workspaces.go:104 | b33706e3eef3c8e3dad25471c02c474688304d36af93faae9eacfd51dd5a5b47 |
| S23 | folderQuery | internal/transport/http/workspaces.go:84 | 7453df6f20d0c780215b6e1563cea0b772f8431a44a1b20f84a00dde472fe337 |
| S24 | workspaceBody | internal/transport/http/workspaces.go:131 | 2664a8378240e673182cdfaa69db59be9bc01a04bc5b0bd97515373b855269fc |
| S25 | writeWorkspaceError | internal/transport/http/workspaces.go:150 | 905f130e4d16c61e342fee1e296ccf859fe5a4e13b99c415c2aea38e7ef01749 |
| S26 | TestWorkspaceFoldersRejectEscapesAndNonGitWithoutInitializingGit | internal/application/workspace_folders_test.go:25 | fdc7459558db7bdb9716940c3560253362ac4c379c0dfe6ddbaafbb8db4ed097 |
| S27 | TestWorkspaceFoldersBoundDirectoryReadsAndHideGitMetadata | internal/application/workspace_folders_test.go:47 | cc696eee0eff5746c6fe626077faf98df10d31dc6607b380097442a982386c9e |
| S28 | collectFolderPages | internal/application/workspace_folders_test.go:63 | 9d019953751c038295828ff37d9b6a84452a2a151baf6e59e6c9ae18983e0b53 |
| S29 | TestWorkspaceFolderValidation | internal/application/workspace_folders_test.go:83 | 6d0d635f265114cf27445ae647cd78e46e08dab8a87fb847805b33a82b789d00 |
| S30 | TestWorkspaceRegistrationCannotPublishAnUnconfiguredManager | internal/application/workspace_folders_test.go:100 | 84c75df0ef5430d2c5d9cb949a78964ca82b1cbf30398fc50690a5126b94ac51 |
| S31 | managedFixture | internal/transport/http/workspaces_integration_test.go:44 | 10a767448df87be02f7ff91afdf46f919e74e817e667e5915951dc5da1a3e9f2 |
| S32 | TestIntegrationWorkspaceAdditionPersistsAndRestoresWithoutEnvironmentEntries | internal/transport/http/workspaces_integration_test.go:67 | c06d16b7978d99d41137920de2df27c1beeb74e5ff0d15859b558a0f5b2a2d11 |
| S33 | assertWorkspaceAccessAndValidation | internal/transport/http/workspaces_integration_test.go:144 | a6209ac436868abe20e2b0e1b10b96501e710aebf564bd9d8da0081ae980c973 |
| S34 | assertAddedWorkspaceAudit | internal/transport/http/workspaces_integration_test.go:161 | 61a0257ff88a861f828da39be2e43f83e589964ec1fed978ad835aa8c399457a |
| S35 | TestWorkspaceRequestRejectsUnknownFieldsAndCrossOriginMutation | internal/transport/http/workspaces_integration_test.go:170 | f053821e82b37681604ccc257e15023b3a78c460e406f9a0cc25829d8f07ff73 |
| S36 | assertUnavailableSavedWorkspace | internal/transport/http/workspaces_integration_test.go:98 | 4935ca55cd1c05cd9b14237c396d5d5bb86ad955d31f465d46c913d0f5cc7417 |
