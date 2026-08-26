# Blind API-only understanding report: workspace_folders.go

## Scope and confidence

Target: `internal/application/workspace_folders.go`.

Repository: `cd106ad658f4cd3b9b86829ca9fef7f62d8be9fb75e0f366755d66aafa197732`.

Snapshot: `70201cf592374b624f0ec7609643cc5979a7aa3d81534a277f4349c96408af82`.

All source citations below refer to captured API evidence at this snapshot, not to the current working tree. T1–T15 identify the full API symbol IDs in the declaration inventory. Other symbols are named with their IDs where introduced.

Confidence is high for the target's own implementation: the file endpoint reported completeness `complete`, and the file-filtered symbols page returned all 15 declarations, with every source excerpt marked `source_truncated:false`. Confidence in repository-wide relationships is necessarily narrower: snapshot diagnostics describe partial static call analysis, not a successful build or runtime execution.

## Purpose

This file implements the application service for browsing operator-allowed server-side folders and registering an existing Git working-tree root as a persistent workspace. It connects HTTP-facing domain contracts to confined filesystem reads, Git validation, the workspace-manager collection, and telemetry. It does not itself parse source, implement SQL, clone repositories, initialize Git, or invoke AI.

The service stores three things: the managed workspace collection, default manager configuration, and the allowed-root list. The server constructs it and passes it as `WorkspaceManagement` to HTTP configuration; the domain interface consists of `FolderRoots`, `Folders`, and `AddWorkspace`. Evidence: target:17–23 (T1/T5); `cmd/server/workspace_runtime.go:29–45`, `managedExplorerServer` ID `fa73dccf6c4347f85093992b0cc2ef75fa74c32605e1cbaeaaa24896943eefe5`; `internal/domain/workspaces.go:55–59`, `WorkspaceManagement` ID `4d0448f9cb4f7dd8a61b4aee6572552e41fd0df5f085631919dea9946f616e38`.

Here and below, “target:line” means `internal/application/workspace_folders.go:line`.

## Declaration inventory

The API inventories one struct, three fields, five functions, five methods, and one closure.

| Ref | Declaration | Target lines | API symbol ID |
|---|---|---:|---|
| T1 | struct `WorkspaceService` | 17–21 | `e3a13c4b6be7e66e1e05d939a9f421e24a9a665d3fbe4c780019929d78951d3a` |
| T2 | field `group` | 18–18 | `1e9e8dee366af2352a447400491e75207289f3aca659fb3948fcd34d3df9424e` |
| T3 | field `defaults` | 19–19 | `632175354a4afd46250d694e27e91f91b285c78ad37eca6e048dd74b6823d84a` |
| T4 | field `roots` | 20–20 | `c5038c1abb2c1cbc69d8ccc89f4df5b2119c276e2b891092d63424fa0cbf9496` |
| T5 | function `NewWorkspaceService` | 23–38 | `4759f37da65a596ae90b4d7ee9fbad884cca2883fe6390399d43132d68aa2c10` |
| T6 | method `*WorkspaceService.FolderRoots` | 40–42 | `a655cf27acc9e43428f64b8ee1863f9dc3b0cf3e1cda210b326d9c3672b5cbee` |
| T7 | method `*WorkspaceService.root` | 44–51 | `628e66c17df805e614df8055067747b19416d478b30ad51aaef8f2a5fd264a6f` |
| T8 | function `validFolderPath` | 53–55 | `014e944000162b649d76e506b53e3a28954be0648f11999eb691d9a04d22aa3c` |
| T9 | method `*WorkspaceService.Folders` | 57–77 | `17ca466ce0140fc95ae155623497e5c11b00de519977364c3b6ffdd675d2b215` |
| T10 | function `readFolderPage` | 79–98 | `349451725d31a5f46fa0b4e0030571e667fa305a7bee8b4512c3a95d564560d6` |
| T11 | function `skipFolderEntries` | 100–115 | `ae71d29d1b81583741a5a31b0ebdc41c762a635bdd6ef111cb598aaf7bac771a` |
| T12 | method `*WorkspaceService.AddWorkspace` | 117–142 | `7d64cc3388edf101b5ec47c97afd6be0aad33ced6371de00d4f00cff67db01ca` |
| T13 | closure `$closure1` | 121–127 | `bba44b7a469a31bf6b538f13c85d5794ed988b2e59b81f4779cd95c5352df730` |
| T14 | function `gitRegistrationError` | 144–149 | `107dfd65166cb43b5a5618204e8c600b28999cb887d80086549f584f9491c50d` |
| T15 | method `*WorkspaceService.selectedPath` | 151–168 | `bf8d3055a2bade2f934c3195443d5fd5a0bcb3ac6c247bc0958a62eb23f9b05c` |

Imports are context, crypto/sha256, fmt, io, os, path/filepath, strings, internal/domain, internal/source/git (aliased `source`), and internal/telemetry. The file endpoint supplied import spans at lines 4–14.

## Contracts and decisions

### Construction and allowed roots

`NewWorkspaceService(group, defaults, paths)` builds a service or returns the first error. Each supplied root is canonicalized, checked with `os.OpenRoot`, closed immediately, and represented as:

- ID: lower-case hexadecimal SHA-256 of the canonical root path.
- Name: `filepath.Base(root)`.
- Path: the canonical server filesystem path.

The constructor retains no open root handles. It preserves input order and does not deduplicate roots. An empty paths slice produces an empty root list. It does not validate `group` for nil; browsing can be used without a group, while successful registration requires a usable group. Root IDs are lookup identifiers, not a substitute for caller authentication. Evidence: target:23–38 (T5); nil-group browsing is explicitly used by `folderFixture`, `internal/application/workspace_folders_test.go:14`, ID `d06f7a9185a3523a5e040c6a716d796fafcb6d26dd3d1fb88b59b13c671e85ba`.

The canonicalization helper rejects an empty configured path, obtains an absolute path, and resolves symlinks. Its errors are generic configured-repository errors; an `os.OpenRoot` failure in this constructor becomes `ErrFolderForbidden`. Evidence: `internal/application/workspaces.go:53–66`, `canonicalWorkspacePath` ID `bc2d241955188c69ad44ae52f26ab8eb3e8039b28f3decacd2f54500062e7fbc`; target:26–33 (T5).

`FolderRoots(context.Context)` ignores the context and returns a copied slice plus nil error. The copy prevents a caller from replacing the service's slice elements. `root(id)` performs exact ID membership lookup and returns `ErrFolderForbidden` when absent. Evidence: target:40–51 (T6/T7).

### Folder path validation and browsing

`validFolderPath` requires a nonempty string of at most 4096 bytes, no NUL rune, and `filepath.IsLocal(path)`. This is the actual lexical policy; the code does not implement a separate literal ban on every `..` component. The 4096 limit is on Go string bytes, not user-visible character count. Evidence: target:53–55 (T8).

`Folders(ctx, id, path, offset)` returns `domain.FolderPage` and error. It first validates the path and inclusive offset range 0–100000, then looks up the allowed root, opens it with `os.OpenRoot`, and opens the selected path through that confined handle. Both handles are deferred closed. The source comment explicitly identifies confinement of every path component, including symlinks, as the reason for using Root. Invalid arguments return a zero page and `ErrInvalidInput`; root lookup/open failures and selected-path open failures return a zero page and `ErrFolderForbidden`. Evidence: target:57–77 (T9).

The page contract is `root_id`, normalized forward-slash `path`, an initialized empty-or-populated `items` array of `{name,path}`, and nullable integer `next_offset`. Evidence: target:79–98 (T10); `internal/domain/workspaces.go:32–42`, `Folder` ID `d97910b0b77aab6471371c34ec51c119e3fd4614b41f6cb2a6cb3d1b1f07cd46`, `FolderPage` ID `0813bc3112c26e16c1efd6d14e0040e128c8c40a11fdbe470c9067a124549bf2`.

Pagination is over raw native directory entries, before filtering:

1. Skip `offset` entries in chunks of at most 100; check context cancellation before each skip chunk.
2. Read at most 100 more raw entries.
3. Keep only entries for which `IsDir()` is true and whose name is not exactly `.git`.
4. Set `next_offset = offset + 100` whenever exactly 100 raw entries were read.
5. Return the page and `ctx.Err()`.

Files and directory symlink entries are omitted from the listing (the OpenAPI description states this explicitly). There is no sort, recursive traversal, or Git-repository test on each listed directory. Other hidden directories are not excluded. Paths in items remain relative to the allowed root. EOF is accepted; other read errors are sanitized to `ErrFolderForbidden`. A cancellation can be returned alongside a constructed page. Evidence: target:79–115 (T10/T11), plus `GET /api/openapi.json` description of `/api/workspace-folders`.

Consequences inferred directly from that control flow:

- A page can contain zero visible folders yet still have a next offset; clients must follow the offset rather than stop on empty items.
- A directory containing an exact multiple of 100 raw entries can require a final empty page.
- Each request reopens the directory and rescans the skipped prefix; work is bounded per request by the accepted offset plus one 100-entry read, but sequential pagination repeats earlier work.
- A full page requested at offset 100000 emits 100100 as the next offset, which the public service rejects. The implementation therefore does not provide indefinite enumeration of arbitrarily large directories.
- Native directory ordering and concurrent directory changes can shift page contents. The API description warns of this; snapshot pinning here applies to the source evidence, not to live folder listings.

### Registration

`AddWorkspace(ctx, request)` returns `domain.AddedWorkspace` or error. The request contains `RootID`, `Path`, and optional `Name`; the result embeds `Repository` and carries `AlreadyAdded`. Evidence: target:117–142 (T12); `internal/domain/workspaces.go:44–53`, request ID `48881377582f34952f75abd5e6609a3b8f3fb4414734a8955d58954179b406df`, result ID `39bdfda83acf4151888728bd8908cd512a67308925a73f95f750fbfe76a26560`.

The sequence is:

1. Start `workspace.add` tracing, emit `execution.started`, and install deferred outcome reporting.
2. Resolve and authorize the selected path.
3. Validate the path with `source.Open(ctx, path, source.Options{})`.
4. Copy the default manager configuration and replace only repository path and repository name; the name is trimmed with `strings.TrimSpace`.
5. Call `s.group.Add(ctx,cfg)`.
6. Return the resulting manager's repository and the group's reuse flag.

`selectedPath` accepts the same path policy as browsing and additionally rejects names longer than 200 bytes or containing NUL. The byte-length check happens before trimming the name. It finds the configured root, resolves symlinks on the joined path, computes the result relative to the root, and requires the resulting relative path to satisfy `filepath.IsLocal`. Missing paths, failed resolution, and an escaped canonical result become `ErrFolderForbidden`. Unlike listing, this path check uses canonical strings rather than an open Root handle passed through subsequent operations. Evidence: target:128–141 (T12), target:151–168 (T15).

Git validation requires the selected path to be the working-tree root, not merely a subdirectory somewhere inside a Git repository. The inspected `source.Open` canonicalizes the path, rejects a terminal trust wildcard, runs Git `rev-parse --show-toplevel`, resolves its returned path, and checks equality with the requested root. It contains no Git initialization. If this validation fails, `gitRegistrationError` preserves context cancellation/deadline errors when present; otherwise it replaces the underlying error with `ErrNotGitRepository`. Evidence: target:132–134 and 144–149 (T12/T14); `internal/source/git/open.go:32–66`, `Open` ID `0dbb93e1e0d9c098c57c2fe33a3de6f2604dc27cdb134daecac4fb2e38505474`.

The target does not check context cancellation before every filesystem operation. Browsing checks during skip and at page completion; registration passes context to Git and manager operations. Consequently, cancellation does not universally outrank argument or filesystem errors.

## Errors, state changes, and observability

### Errors

The target directly returns `ErrInvalidInput`, `ErrFolderForbidden`, `ErrNotGitRepository`, context errors, constructor canonicalization errors, and errors propagated from `Workspaces.Add`. `ErrFolderForbidden` deliberately covers several distinct situations, including an unknown root, missing/unreadable selected path, non-directory read failure, and actual root escape; it is not proof that an attempted path escaped.

The group can additionally return cancellation for a closed group, `ErrWorkspaceLimit`, `ErrNotConfigured`, manager-creation errors, or persistence errors. Evidence: `internal/application/workspaces_persistence.go:75–107`, `Workspaces.Add` ID `ad0f0c7a658baf1a964c090eac9650c3022b437cd79d5c5f847ef982ff97da30`.

At HTTP boundaries, non-Git maps to 422/`not_git_repository`, forbidden folder to 403/`folder_forbidden`, and workspace capacity to 409/`workspace_limit`; other errors use the shared error mapper. Evidence: `internal/transport/http/workspaces.go:150–161`, `writeWorkspaceError` ID `905f130e4d16c61e342fee1e296ccf859fe5a4e13b99c415c2aea38e7ef01749`. The integration-test source expects invalid traversal input to return 400.

### Registration side effects

The target delegates durable and in-memory changes:

- `Workspaces.Add` holds its mutex, reuses an existing manager with the same canonical path before checking capacity, limits the in-memory collection to 32, requires a WorkspaceStore, constructs a manager, persists registration, then appends it. Persistence failure closes the new manager. The first manager becomes fallback; an already-started group starts the new manager with the group's lifecycle context. A reused manager does not get renamed by the new request.
- Manager initialization reopens Git, calculates repository ID from the canonical path, defaults an empty name to the directory basename, calls `EnsureRepository`, reads the latest snapshot, and sets status ready. Thus the broader registration sequence is not one single atomic transaction over all manager/database operations: catalog initialization precedes durable workspace registration.
- Immediately before publication, `persistAddedWorkspace` verifies nonnil manager source and exact root equality with the expected selected path. It constructs an `operator` / `workspace.add` execution and calls `SaveWorkspaces`.
- The PostgreSQL adapter validates records, starts a transaction, takes a transaction-scoped advisory lock, counts the union of existing and incoming workspace IDs, rejects more than 32, upserts workspace roots, calls the audit writer, and commits. This supplies a cross-process capacity check in addition to the group's in-memory one.

Evidence: `internal/application/workspaces_persistence.go:75–126`, `Add` ID above, `existingPath` ID `aa0f9a2cd0caa90690c6e93905cf943430fddddaedec405b6783831ad82a9ee9`, `persistAddedWorkspace` ID `fb8908be485d6deabd7f89abd896bbf9cd8ac8fe847695d5a14fe12599b9a01d`; `internal/application/manager.go:78–103`, `initialize` ID `b0cc7715ef7032ce3857beaf95c92ccb9d5bc16060d30d0d72900f6c8e8739b5`; `internal/store/postgres/workspaces.go:29–51`, `SaveWorkspaces` ID `c9380b7c16dd2dcfd0ac0f8cb763b4e3fdb5366dc18ca5540bbc263251e53720`, and :65–87, `saveWorkspaceRecords` ID `af644ea9b16d3313e6ba36e3483edebdcd853400ec6f56dfa0f18a53145e6617`.

No source mutation occurs in the target's browse/validation code. A source comment in the HTTP handler says initialization of repository services does not make an inference request. I did not inspect the complete manager lifecycle, so I do not claim all later background behavior is absent. I also did not inspect the audit writer body; the call and a concrete SQL audit assertion in the integration test support the durable-audit account.

### Telemetry

The named `err` result is observed by the deferred closure: failure calls `telemetry.Fail(span,"workspace_registration_failed")`; success emits `execution.completed`. Because the outcome defer is registered after `span.End`, the outcome event executes before the span ends. No source, path, credentials, or prompt is explicitly added as a span attribute in this method. Browse/root listing methods have no explicit tracing in this file. Evidence: target:117–127 (T12/T13).

The delegated execution builder makes a new execution ID, records actor/trigger, and copies the current TraceID if valid. It also creates a context without cancellation, although `persistAddedWorkspace` extracts only the execution and still passes its original context to storage. Evidence: `internal/application/manager_queue.go:45–53`, `newScanRequest` ID `83e159e2c149e639b79a542350a6be7305c80da5ea8feabf98c52a14df1adcbb`.

The inspected registration-success path persists audit; the target's failure closure only reports telemetry. I did not establish a durable audit guarantee for failed registration attempts.

## Callers, callees, and integration boundaries

### API-confirmed resolved call edges

The `/calls` endpoint explicitly marked these edges resolved:

- `managedExplorerServer` -> `NewWorkspaceService` at `cmd/server/workspace_runtime.go:31`.
- `Folders` -> `validFolderPath`, `root`, and `readFolderPage` at target:58, 61, 76.
- `AddWorkspace` -> `scanTracer` (ID `b953c30ba1fc2a56801fd9f74f795f6039f03369c894f1cc5c96dcc04a7fec3c`), its deferred closure, `selectedPath`, Git `Open`, `gitRegistrationError`, `Workspaces.Add`, and `Manager.Repository` (ID `d21032b09cbbb3e715c9598beade67bafc6e8380543b399068672142784303a3`) at target:118, 121, 128, 132, 133, 137, 141.

These call pages were complete for their requested symbols (11 outgoing AddWorkspace calls, seven outgoing Folders calls, one incoming constructor call). They are static bindings, not an execution-order trace.

### Textual/interface integration, not resolved implementation edges

HTTP mounts authenticated `GET /api/workspace-roots`, `GET /api/workspace-folders`, and `POST /api/repositories`. The handlers invoke the corresponding WorkspaceManagement methods. Server construction assigns the concrete WorkspaceService to that configuration field, so the excerpts establish the intended wiring. Evidence: `internal/transport/http/workspaces.go:22–41`, `mountManagedWorkspaces` ID `04a3e9ebbecde5264ff8caa15d7d768682ab69a16db63480ee987e68d866cdc3`; :65–82, roots ID `e9eb667f9576ba154e6f0e31af67c9cd393c8d66b218c11dd6b087189944a62a`, folders ID `e50875ac6c5bdec77ac88a5ca3cadc8e98552675b13b764a40d07267f25fb927`; :104–129, add ID `b33706e3eef3c8e3dad25471c02c474688304d36af93faae9eacfd51dd5a5b47`.

However, the HTTP add call at line 114 has empty target ID and reason `interface_or_type_parameter_dispatch`; it is not a resolved graph edge to the concrete target method. Its call ID is `6f891555d61bb643aa6624f6dde3887534cb923485c0a7a09a0b22acd1de2e9d`. The runtime Explorer call is similarly unresolved. Standard-library and telemetry calls also often have empty targets because imported bindings are unavailable. I have not treated these gaps as absent functionality.

HTTP adds transport policy beyond the application service: authentication middleware, same-origin mutation checks, JSON content-type validation, an 8192-byte body cap, rejection of unknown fields/trailing JSON, and required nonempty root/path. Success initializes the runtime explorer and returns 201 for a new workspace or 200 for reuse. Explorer initialization can fail after the service has registered the workspace; the handler's error response therefore does not imply rollback of the preceding registration. Evidence: HTTP file :104–148, add ID above and `workspaceBody` ID `2664a8378240e673182cdfaa69db59be9bc01a04bc5b0bd97515373b855269fc`.

Authentication is a transport responsibility here: the target's root list includes absolute server paths, and the service has no per-user authorization logic beyond allowed-root membership.

## Test evidence retrieved, not executed

All statements in this section describe captured test source only. I ran no test, build, repository code, Git command, database query, or UI.

| Captured test/helper | Evidence in source |
|---|---|
| `internal/application/workspace_folders_test.go:25`, `TestWorkspaceFoldersRejectEscapesAndNonGitWithoutInitializingGit`, ID `fdc7459558db7bdb9716940c3560253362ac4c379c0dfe6ddbaafbb8db4ed097` | Creates an outside symlink; rejects parent escape, absolute path, escaping symlink, and missing path for browse and add; expects ErrNotGitRepository for a non-Git root and verifies .git was not created. |
| Same file:47, `TestWorkspaceFoldersBoundDirectoryReadsAndHideGitMetadata`, ID `cc696eee0eff5746c6fe626077faf98df10d31dc6607b380097442a982386c9e`; helper:63, ID `9d019953751c038295828ff37d9b6a84452a2a151baf6e59e6c9ae18983e0b53` | Creates 105 folders and .git; follows next offsets, asserts each visible page <=100, and expects all 105 ordinary folders with no .git. |
| Same file:83, `TestWorkspaceFolderValidation`, ID `6d0d635f265114cf27445ae647cd78e46e08dab8a87fb847805b33a82b789d00` | Rejects offsets -1 and 100001, rejects unknown root, preserves context.Canceled at offset zero. |
| Same file:100, `TestWorkspaceRegistrationCannotPublishAnUnconfiguredManager`, ID `84c75df0ef5430d2c5d9cb949a78964ca82b1cbf30398fc50690a5126b94ac51` | Calls publication guard with an unconfigured manager and expects ErrFolderForbidden. |
| `internal/transport/http/workspaces_integration_test.go:67`, `TestIntegrationWorkspaceAdditionPersistsAndRestoresWithoutEnvironmentEntries`, ID `c06d16b7978d99d41137920de2df27c1beeb74e5ff0d15859b558a0f5b2a2d11` | Uses PostgreSQL/temporary-Git fixtures, adds with 201, repeats with 200/reused ID, waits for snapshot, checks default routing, restarts/restores persisted registration, observes a later source change, and checks saved snapshots remain readable when checkout disappears. Fixture implementations outside this file were not inspected. |
| Same integration file:144, `assertWorkspaceAccessAndValidation`, ID `a6209ac436868abe20e2b0e1b10b96501e710aebf564bd9d8da0081ae980c973` | Expects unauthenticated access to receive 401; non-Git/escape/unknown-root additions 422/400/403; verifies failed validation leaves workspace count zero. |
| Same integration file:161, `assertAddedWorkspaceAudit`, ID `61a0257ff88a861f828da39be2e43f83e589964ec1fed978ad835aa8c399457a` | SQL assertion requires exactly one workspace.add audit record for the repository, actor operator, nonempty trace ID, and completed outcome, despite duplicate registration. |
| Same integration file:170, `TestWorkspaceRequestRejectsUnknownFieldsAndCrossOriginMutation`, ID `f053821e82b37681604ccc257e15023b3a78c460e406f9a0cc25829d8f07ff73` | Exercises malformed/body-size/trailing/unknown-field cases and foreign Origin rejection. The body requests shown do not set Content-Type, so the excerpts alone do not isolate each decoder rejection from the earlier content-type guard. |

I did not establish test coverage for names at the byte boundary, internal symlink acceptance, exact-multiple/empty-filtered pages, offset-100000 continuation, concurrent path replacement, nil group on valid registration, or every lower-level storage failure. These are coverage limits of this investigation, not claims those tests do not exist elsewhere.

## Uncertainties and limits

1. Source completeness and call completeness differ. Snapshot `/details` reports call analysis `partial`, 1347 resolved and 9730 unresolved calls. Its limitations include static local binding only; test/platform/conditional files excluded from typed selection; standard/external imports unavailable; interface/function-value/type-parameter dispatch unresolved; fixed gc/amd64 sizing; and no guarantee of compilation or argument compatibility.
2. Test files are indexed and readable, but their callsites are excluded from deterministic typed selection. That explains why tests need text/symbol retrieval rather than reliance on resolved caller edges.
3. I did not inspect every snapshot diagnostic: the details response was large and truncated by the command-output cap. I inspected its analysis summary/limitations and selected beginning/end excerpts.
4. Root-handle browsing is directly visible. Registration instead resolves path strings, reopens Git, and revalidates the actual manager root before publication. These checks are clear, but I have not proved safety under every filesystem race and performed no adversarial runtime testing.
5. The service assumes valid construction/context/group dependencies. It does not validate a nil group or context. No runtime behavior for invalid dependencies was tested.
6. Broader manager lifecycle, complete SQL audit implementation, failure audit coverage, and background model behavior were outside the inspected boundary. There is no AI request in the target.
7. No saved model reviews, inferred features, or generated answers were needed or requested. I used source evidence and deterministic relationships only.

## API experience

Most useful endpoints:

- `/api/openapi.json`: identified the safe GET surface, snapshot-qualified routes, pagination limits, coordinate conventions, and the partial-analysis caveats.
- Snapshot `/file?path=...`: compact package/import inventory and file-level completeness.
- Snapshot `/symbols?file=...&limit=100&offset=0`: the decisive endpoint; supplied complete target declarations and source excerpts in one bounded page. Targeted `q` searches located helpers; symbol-ID detail supplied exact callees and startup wiring.
- Snapshot `/calls`: resolved local bindings, exact callsite spans, and explicit reasons for interface/import gaps.
- Snapshot `/files?q=workspace&limit=100&offset=0`: found related tests and transport/persistence files in a 24-file result.
- Snapshot `/details`: established analysis limitations, although much of its response was not needed for this file.

Difficulties:

- The OpenAPI document was 394247 bytes. Requesting the whole document through a capped command-output channel produced truncation and redundant retrieval. A compact GET-endpoint index generated in memory solved this. A smaller API discovery endpoint or reusable response-cache facility would make this cheaper.
- The first un-escalated loopback attempt returned no HTTP response; escalated reads then worked. The exact initial exception text was not retained, so its cause is not asserted as a server failure.
- One early discovery request's exact metadata was lost when its output began with a truncation warning and the wrapper attempted JSON parsing before saving it. This is an explicit ledger defect, not an estimated success.
- The 93473-byte details response is not narrowly filtered by target and was truncated in output. Its summary was enough; I did not download it again.
- Symbol responses repeat source/signatures, parent/field data, spans, and hashes. Compact in-memory projections substantially reduced reading overhead for subsequent related files.
- Search is broader than exact name matching: `q=NewManager&kind=function` also returned `newManagerStore` from a test file. I inspected that extra small result but did not rely on it.

## Request accounting

26 HTTP GET attempts were made, all to exactly `http://127.0.0.1:18089`. There were 24 confirmed HTTP 200 responses, one confirmed no-response attempt, and one discovery attempt with unknown retained outcome.

Known response-body bytes total **1056093 bytes**. Known client HTTP elapsed times sum to **256.20 ms**. Both are lower bounds for the complete experiment because attempt 2's byte/time metrics were lost. These times measure request/read duration only, not approval latency, command startup, reasoning, or overall wall-clock duration; parallel request durations are summed, not presented as wall time.

All 22 source/context HTTP requests used the exact supplied repository/snapshot prefix. The other four attempts were API discovery at `/api/openapi.json`; no live-folder browsing, latest-snapshot fallback, or unpinned source endpoint was used.

Inspected versus received:

- Read the source excerpts for all 15 target declarations; no target excerpt was truncated.
- Across related retrievals, inspected 99 symbol excerpt instances (including repeated declarations, fields, closures, and one incidental search result), containing about 33185 source characters across 13 source paths.
- Approximately 146954 characters of successful structured response/projection text were presented and inspected, plus about 9000 characters of selected prefixes/suffixes from the oversized discovery/details outputs. This is an inspection-size approximation, not a claim every returned byte was read.
- Did not inspect the full OpenAPI schema bodies or the entire details diagnostic/skip lists. They dominate the received-byte cost.

The accompanying JSON ledger includes every attempt with method, exact route/query, status, raw body bytes, and elapsed milliseconds when retained. Null metrics remain null rather than being reconstructed. Batch rows follow planned request order; concurrently issued requests may have completed in another order.

## Protocol compliance

No workspace files, local documentation, tests, AGENTS.md, Git metadata, database contents, shell profiles, existing temporary files, other agents' source notes, external sites, or UI state were read directly. All project source and test text came from the specified API origin. Shell commands used inline Python HTTP clients with non-login shells; no repository code, builds, tests, or Git commands were executed. No HTTP mutation, inference, generation, or non-GET request was made.

The only filesystem writes are this self-authored report and its request ledger at the two permitted clean temporary output paths. No repository file was changed. No authentication credential is included in either artifact.

No source-access restriction was violated. The one disclosed deviation is incomplete per-request measurement retention for discovery attempt 2; attempt 1's failure exception detail was also lost, while its no-response metrics were retained. This report is the first independent interpretation and was completed without receiving the parent's source findings.
