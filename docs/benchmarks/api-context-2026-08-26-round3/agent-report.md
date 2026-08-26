# Blind API-only navigation report: `internal/application/workspace_folders.go`

## Scope and evidence status

This is the first independent report for repository `cd106ad658f4cd3b9b86829ca9fef7f62d8be9fb75e0f366755d66aafa197732`, pinned to snapshot `e3861b1c739b7e83f448dee146af2a8dc539d1dd729ab6bb09468b1083114e4e`. I used only the supplied read-only snapshot API. I did not read the checkout, Git metadata, local documentation, a database, a browser, external sites, profiles, or another agent's work, and I did not run commands against the repository, builds, or tests.

The snapshot API reports `source_complete: true`, snapshot completeness `complete`, 237 files, 3,377 symbols, and 15 packages. It also reports a dirty `main` snapshot with no recorded commit and `changes_known: false`. Its analysis level is syntax based. Call analysis is explicitly partial: local static bindings may be resolved, but standard-library/external bodies, interface dispatch, and function values may be unresolved, and tests are excluded from deterministic typed call selection. Implementation matches based on `signature_match_with_unavailable_types` are conditional only. These limitations matter throughout this report.

Evidence labels used below:

- **Direct**: returned source, symbol metadata, call metadata, or snapshot metadata.
- **Wiring inference**: a conclusion from inspected construction, assignment, and control flow, rather than from an implementation-candidate record alone.
- **Unknown**: not established by the API evidence or requires runtime state.

All line numbers are physical, one based snapshot lines. Symbol IDs identify the exact declarations returned by the pinned API.

## Purpose and behavior in one view

`workspace_folders.go` exposes a configured set of server filesystem roots to authenticated HTTP workspace-management endpoints. It has two related responsibilities:

1. It offers bounded, read-only directory browsing below configured roots. Paths must be local relative paths; offsets are bounded; pages consume at most 100 raw directory entries; files, `.git`, and symlinks are not published as child folders.
2. It registers an existing Git worktree as a managed workspace. It resolves the selected path, rechecks containment below a configured root, requires that the selection be the exact root of an existing Git worktree, persists the workspace and a completed audit event, returns an existing manager idempotently for duplicate canonical paths, and starts background scanning for a newly added manager when the manager group is already running.

It does not upload files, initialize or clone a repository, wait for indexing, or directly invoke a model. The browsing side does not mutate the filesystem. Registration has database and background-processing side effects described below.

## Complete declaration inventory for the target file

The file API returned 15 declarations: one struct, three fields, five functions, five methods, and one closure. No target source capture was truncated. Consequently, every executable declaration's full body was inspected through the API; the struct and fields have declarations but no executable body.

| Declaration | Symbol ID and span | Parameters / result or type | Inspection status |
|---|---|---|---|
| `WorkspaceService` | `e3a13c4b6be7e66e1e05d939a9f421e24a9a665d3fbe4c780019929d78951d3a`, `internal/application/workspace_folders.go:17-21` | struct | Complete declaration inspected; no body |
| field `group` | `1e9e8dee366af2352a447400491e75207289f3aca659fb3948fcd34d3df9424e`, line 18 | `*Workspaces` | Complete declaration inspected; no body |
| field `defaults` | `632175354a4afd46250d694e27e91f91b285c78ad37eca6e048dd74b6823d84a`, line 19 | `ManagerConfig` | Complete declaration inspected; no body |
| field `roots` | `c5038c1abb2c1cbc69d8ccc89f4df5b2119c276e2b891092d63424fa0cbf9496`, line 20 | `[]domain.FolderRoot` | Complete declaration inspected; no body |
| `NewWorkspaceService` | `4759f37da65a596ae90b4d7ee9fbad884cca2883fe6390399d43132d68aa2c10`, lines 23-38 | `(group *Workspaces, defaults ManagerConfig, paths []string) (*WorkspaceService, error)` | Full body API-inspected; complete |
| `(*WorkspaceService).FolderRoots` | `a655cf27acc9e43428f64b8ee1863f9dc3b0cf3e1cda210b326d9c3672b5cbee`, lines 40-42 | `(context.Context) ([]domain.FolderRoot, error)` | Full body API-inspected; complete |
| `(*WorkspaceService).root` | `628e66c17df805e614df8055067747b19416d478b30ad51aaef8f2a5fd264a6f`, lines 44-51 | `(id string) (domain.FolderRoot, error)` | Full body API-inspected; complete |
| `validFolderPath` | `014e944000162b649d76e506b53e3a28954be0648f11999eb691d9a04d22aa3c`, lines 53-55 | `(path string) bool` | Full body API-inspected; complete |
| `(*WorkspaceService).Folders` | `17ca466ce0140fc95ae155623497e5c11b00de519977364c3b6ffdd675d2b215`, lines 57-77 | `(ctx context.Context, id, path string, offset int) (domain.FolderPage, error)` | Full body API-inspected; complete |
| `readFolderPage` | `349451725d31a5f46fa0b4e0030571e667fa305a7bee8b4512c3a95d564560d6`, lines 79-98 | `(ctx context.Context, folder *os.File, id, path string, offset int) (domain.FolderPage, error)` | Full body API-inspected; complete |
| `skipFolderEntries` | `ae71d29d1b81583741a5a31b0ebdc41c762a635bdd6ef111cb598aaf7bac771a`, lines 100-115 | `(ctx context.Context, folder *os.File, offset int) error` | Full body API-inspected; complete |
| `(*WorkspaceService).AddWorkspace` | `7d64cc3388edf101b5ec47c97afd6be0aad33ced6371de00d4f00cff67db01ca`, lines 117-142 | `(ctx context.Context, request domain.AddWorkspaceRequest) (result domain.AddedWorkspace, err error)` | Full body API-inspected; complete |
| deferred closure in `AddWorkspace` | `bba44b7a469a31bf6b538f13c85d5794ed988b2e59b81f4779cd95c5352df730`, lines 121-127 | captures named `err` and span | Full body API-inspected; complete |
| `gitRegistrationError` | `107dfd65166cb43b5a5618204e8c600b28999cb887d80086549f584f9491c50d`, lines 144-149 | `(ctx context.Context) error` | Full body API-inspected; complete |
| `(*WorkspaceService).selectedPath` | `bf8d3055a2bade2f934c3195443d5fd5a0bcb3ac6c247bc0958a62eb23f9b05c`, lines 151-168 | `(request domain.AddWorkspaceRequest) (string, error)` | Full body API-inspected; complete |

The associated domain types are directly declared in `internal/domain/workspaces.go`: `FolderRoot` (`e8cb90c5...`, lines 26-30) has `ID`, `Name`, and `Path`; `Folder` (`d97910...`, lines 32-35) has `Name` and `Path`; `FolderPage` (`0813bc...`, lines 37-42) has `RootID`, `Path`, `Items`, and optional `NextOffset`; `AddWorkspaceRequest` (`488813...`, lines 44-48) has `RootID`, `Path`, and `Name`; and `AddedWorkspace` (`39bdfd...`, lines 50-53) embeds `Repository` and adds `AlreadyAdded`. `domain.Repository` (`4170193213afd888c0037e99a9cb79ca8a9405576fea9fda8013abb50c3dd9c1`, `internal/domain/catalog.go:23-26`) contains `ID` and `Name`.

## Construction and root identity

**Direct.** `NewWorkspaceService` constructs a service with the supplied manager group and default manager configuration, and begins with an empty, non-nil root slice (`internal/application/workspace_folders.go:23-38`, ID `4759f37...`). For every supplied path it:

1. Calls `canonicalWorkspacePath`.
2. Calls `os.OpenRoot` on the canonical result as an accessibility/confinement probe.
3. Closes that root immediately, ignoring a close error.
4. Appends a `FolderRoot` whose ID is lowercase hexadecimal SHA-256 of the canonical path string, whose name is `filepath.Base(canonical)`, and whose path is the canonical path.

`canonicalWorkspacePath` was also inspected in full (`internal/application/workspaces.go:53-66`, ID `bc2d241955188c69ad44ae52f26ab8eb3e8039b28f3decacd2f54500062e7fbc`). It rejects an empty configured path, makes the path absolute, then resolves symlinks. Absolute-path or symlink-resolution failures become a configured-repository-unavailable error. An `os.OpenRoot` failure in the constructor is instead mapped to `domain.ErrFolderForbidden`. The constructor returns immediately on the first failure, with no partially constructed service returned.

The constructor itself does not cap or deduplicate `paths`, and it does not reject a nil `group`. Repeating a canonical path therefore repeats the root entry with the same derived ID. A nil group does not affect browsing but would be dereferenced by registration. These are direct consequences of the body, not evidence that production supplies such inputs.

**Direct configuration boundary.** The production config helper `loadWorkspaceRoots` (`internal/config/workspaces.go:10-33`, ID `1207ff...`) imposes constraints on explicitly configured `WORKSPACE_ROOTS`: the raw value is limited to 65,536 bytes; it must decode as a non-null JSON array; at most 16 roots are accepted; each must be absolute, at most 4,096 bytes, and contain no NUL. Those limits are not invariants of `NewWorkspaceService` itself.

`workspaceRootPaths` (`cmd/server/workspace_runtime.go:100-110`, ID `b0850...`) returns explicit configured roots when present; otherwise it derives roots from the configured repository managers' repository paths. `managedExplorerServer` (`cmd/server/workspace_runtime.go:29-45`, ID `fa73dcc...`) passes those paths, the shared `*Workspaces` group, and manager defaults into `NewWorkspaceService`, then assigns the service to the HTTP transport's `WorkspaceManagement` field. This is inspected production construction evidence, not merely a method-signature match.

`FolderRoots` (`workspace_folders.go:40-42`, ID `a655cf...`) ignores its context and returns a shallow copy of the slice with a nil error. Callers cannot replace or append into the service's backing slice through the returned slice. `FolderRoot` contains strings, so no nested mutable members are shared.

`root` (`workspace_folders.go:44-51`, ID `628e66...`) performs a linear, first-match lookup by exact ID. No match, including an empty or malformed ID, returns an empty root and `domain.ErrFolderForbidden` (`internal/domain/workspaces.go:11`, ID `5cfacd...`). There is no separate unknown-root error.

## Folder browsing rules

### Input and confinement

`validFolderPath` (`workspace_folders.go:53-55`, ID `014e94...`) accepts a path only when all of these hold:

- it is nonempty;
- its Go string length is at most 4,096 bytes;
- it contains no NUL byte;
- `filepath.IsLocal(path)` returns true.

`Folders` (`workspace_folders.go:57-77`, ID `17ca46...`) returns `domain.ErrInvalidInput` (`internal/domain/intelligence.go:14`, ID `dc84cd...`) if that path check fails or if `offset` is outside inclusive range 0 through 100,000. It then resolves the root ID; opens a fresh `os.Root` for the configured canonical root; opens the requested relative path through that root; and defers both closes. Root-open or folder-open errors are uniformly mapped to `ErrFolderForbidden`. The source comment at line 70 says the root confines every path component, including symlinks, during filesystem reads.

The standard-library implementation of `os.OpenRoot`/`os.Root.Open` was unavailable to this snapshot analysis. Therefore the confinement statement is directly present as application intent and the code uses the confinement API, but I did not independently inspect the standard library's platform-specific implementation. Filesystem permissions, races, mount behavior, and platform path rules remain runtime dependent.

### Paging, filtering, and cancellation

`readFolderPage` (`workspace_folders.go:79-98`, ID `349451...`) initializes a page with:

- the supplied root ID;
- a slash-normalized `filepath.Clean(path)`;
- a non-nil empty item slice.

It first calls `skipFolderEntries`. That helper (`workspace_folders.go:100-115`, ID `ae71d2...`) consumes raw directory entries in batches of at most 100 until the requested offset is exhausted. It checks `ctx.Err()` before each skip batch. An EOF while skipping means success at end of directory; any other read error becomes `ErrFolderForbidden`. Given the service's 100,000 offset bound, at most 1,000 skip reads are requested.

The page reader then calls `ReadDir(100)` exactly once. A non-EOF error becomes `ErrFolderForbidden`. It publishes only entries for which `entry.IsDir()` is true and whose name is not exactly `.git`. Each published item contains the entry name and `filepath.ToSlash(filepath.Join(path, name))`. It does not sort; pagination is over the filesystem's native raw-entry order. Files and filtered entries still consume offset/page capacity.

If exactly 100 raw entries were returned, `NextOffset` is set to `offset + 100`, even if filtering left the published item list empty and even if no later entry exists. Otherwise it is nil. Consumers must follow `NextOffset` rather than infer completion from item count. Directory changes between requests can shift or duplicate entries because there is no stable snapshot.

Finally, the function returns the constructed page together with `ctx.Err()`. Cancellation can thus return a partially or fully constructed page plus a context error. With offset zero, there is no pre-read cancellation check; cancellation is observed at the final return. With a positive offset it is also checked between skip batches.

The registered OpenAPI description (`internal/transport/http/openapi_routes.go:5-16`, ID `73d461...`) agrees with these mechanics: a page consumes at most 100 raw native-order entries, omits files, `.git`, and symlinks, requires clients to follow `next_offset`, and warns that directory changes shift pages. The route is described as browsing only, with no upload or inference side effect.

## Workspace registration

### Selection validation

`selectedPath` (`workspace_folders.go:151-168`, ID `bf8d30...`) applies the following decision sequence:

1. The requested relative path must pass `validFolderPath`.
2. `len(request.Name)` must be at most 200 bytes and the name must contain no NUL. An empty or whitespace-only name is allowed here.
3. The root ID must resolve through `root`.
4. `filepath.Join(root.Path, request.Path)` is resolved with `filepath.EvalSymlinks`; failure becomes `ErrFolderForbidden`.
5. The resolved path is made relative to the configured root. A `filepath.Rel` error, or a relative result for which `filepath.IsLocal` is false, becomes `ErrFolderForbidden`.

These checks make the selected path existing and, after symlink resolution, within the configured root according to the platform filepath rules. The returned value is the resolved path. Selection does not itself require a directory or Git repository; the subsequent Git open supplies that requirement.

### Existing-Git requirement and error mapping

`AddWorkspace` (`workspace_folders.go:117-142`, ID `7d64cc...`) begins a `workspace.add` application span and emits a started event before selection. It calls `source.Open(ctx, path, source.Options{})` and discards the successful repository object: this first open is a validation probe.

`source.Open` was inspected in full (`internal/source/git/open.go:32-66`, ID `0dbb93...`). It makes the path absolute, resolves symlinks, rejects filesystem root, runs Git `rev-parse --show-toplevel`, resolves symlinks on Git's returned top-level directory, and requires that top-level directory to equal the selected root. Thus registration accepts an existing Git worktree only when the selected folder is exactly its top level. It does not initialize or clone Git.

Any error from that probe is collapsed through `gitRegistrationError` (`workspace_folders.go:144-149`, ID `107dfd...`): if the context currently has an error, that context error is returned; otherwise the result is `domain.ErrNotGitRepository` (`internal/domain/workspaces.go:9`, ID `68904...`). Low-level filesystem and Git failure details are intentionally not returned from this service path.

After validation, `AddWorkspace` copies the default manager configuration, replaces `RepositoryPath` with the selected canonical path, and sets `RepositoryName` to `strings.TrimSpace(request.Name)`. It then calls `s.group.Add`. Whitespace-only names become empty; manager initialization subsequently falls back to the canonical path's base name. On a duplicate canonical path, the existing manager and its existing repository name are returned; the new request does not rename it.

The successful result is `domain.AddedWorkspace{Repository: manager.Repository(), AlreadyAdded: reused}`. The deferred closure (`workspace_folders.go:121-127`, ID `bba44b...`) marks the span failed with stable reason `workspace_registration_failed` whenever the named return error is non-nil; otherwise it emits the completed event. It always ends the span.

### Idempotence, capacity, persistence, and partial side effects

The concrete `(*Workspaces).Add` body was inspected (`internal/application/workspaces_persistence.go:75-107`, ID `ad0f0c...`). It holds the group lock across the operation. Its decisions are ordered as follows:

1. A closed group returns `context.Canceled`.
2. An existing manager with exactly equal canonical `RepositoryPath` is returned with `reused=true`. This check occurs before capacity and persistence.
3. A group with 32 managers returns `domain.ErrWorkspaceLimit` (`internal/domain/workspaces.go:10`, ID `a59d...`).
4. The configured store must implement `WorkspaceStore`; otherwise it returns a not-configured error.
5. It creates a new manager.
6. It persists the workspace registration. On failure, it closes the new manager and does not append it to the group.
7. It appends the manager, establishes a fallback if needed, and, if the group is already started, calls `manager.Start`.

`NewManager` (`internal/application/manager.go:58-76`, ID `df0e...`) supplies a two-second default poll interval and 30-second default scan timeout, then initializes immediately when `RepositoryPath` is nonempty. `initialize` (`manager.go:78-103`, ID `b0cc...`) opens the Git source again, stores its canonical root back in config, derives the repository ID as lowercase SHA-256 of that canonical root, chooses the trimmed configured name or the root base name, calls `store.EnsureRepository`, reads the latest snapshot, and marks the manager ready. `Manager.Repository` (`manager.go:105`, ID `d210...`) returns that stored domain repository.

`persistAddedWorkspace` (`internal/application/workspaces_persistence.go:119-126`, ID `fb8908...`) refuses persistence if the manager has no source or its source root no longer exactly equals the expected selected root. It creates `workspace.add` execution metadata with actor `operator`; `newScanRequest` (`internal/application/manager_queue.go:45-53`, ID `83e159...`) generates an execution ID and copies a valid current trace ID. The save itself still receives the original request context.

The production database wiring was directly inspected. `startExplorer` (`cmd/server/main.go:58-74`, ID `9c021...`) passes a `*postgres.Store` into `NewPersistentWorkspaces` (`internal/application/workspace_persistence.go:11-25`, ID `82bab...`), which requires `WorkspaceStore` and then calls `NewWorkspaces`; `NewWorkspaces` (`internal/application/workspaces.go:23-33`, ID `3beb...`) stores it in the group. This construction establishes the production receiver independently of implementation-match metadata.

The implementation endpoint returned one candidate for `WorkspaceStore.SaveWorkspaces`: `(*postgres.Store).SaveWorkspaces` (`internal/persistence/postgres/workspaces.go:29-51`, ID `c9380...`). Its match evidence was only `signature_match_with_unavailable_types`, so that record alone is conditional; the production assignment above supplies the receiver wiring. The body validates the registration/audit records, returns early for no records, marshals them, begins a transaction, saves the records, and commits.

`saveWorkspaceRecords` (`postgres/workspaces.go:65-87`, ID `af644...`) takes a transaction-scoped advisory lock, counts the union of current workspace repository IDs and payload IDs, enforces the same maximum of 32, upserts the workspace records, and calls `auditWorkspaceRegistration`. The audit helper (`postgres/workspaces.go:89-96`, ID `3c4a9...`) writes completed registration audit events and audit-outbox records in that same transaction, carrying actor, trigger, and trace metadata. `validExecution` (`internal/persistence/postgres/audit.go:43-51`, ID `766969...`) validates the relevant execution, actor, trigger, trace, and outcome fields.

There is a narrower atomicity boundary before this registration transaction. Manager initialization calls concrete `(*postgres.Store).EnsureRepository` (`internal/persistence/postgres/write.go:19-28`, ID `6eac7...`), which performs its own repository insert/upsert operation before `SaveWorkspaces` begins. If the later registration save fails, the new manager is closed and not published in memory, but the earlier catalog repository upsert is not part of the workspace-registration transaction and may remain. Successful workspace plus completed audit/outbox writes are transactional together. The inspected path does not create a durable failed-registration audit record; failure is represented in telemetry.

### Background boundary

If the group is running, `Workspaces.Add` calls `Manager.Start` after successful durable registration and after appending the manager. `Manager.Start` (`internal/application/manager.go:123-135`, ID `a45e29...`) is guarded against duplicate start, closed managers, and absent sources; it creates a cancellable context, sets scanning status, increments its wait group, and launches `go m.run(runCtx)`.

`Manager.run` (`internal/application/manager_queue.go:65-86`, ID `5a244...`) performs an immediate startup scan and then services the polling timer, manual scan requests, and watch-triggered scans until cancellation. Therefore registration does not wait for indexing or scan completion. It returns after the goroutine has been launched. No model request occurs in the inspected registration/control path.

## Telemetry and operation boundaries

The application tracer returned by `scanTracer` is named `developa/application` (`internal/application/scan.go:134-136`, ID `b953...`). `AddWorkspace` creates its `workspace.add` span before validation, emits start and success/failure events, records a stable failure type through `telemetry.Fail`, and ends the span. `telemetry.Fail` (`internal/telemetry/telemetry.go:88-91`, ID `857c...`) sets error status and emits `execution.failed` with a stable `error.type`.

PostgreSQL operations use `operation` (`internal/persistence/postgres/operations.go:16-29`, ID `925dd...`): it applies a 30-second timeout, starts a `developa/postgres` span, emits started/completed events, and records failures as `catalog_operation_failed`. A valid application trace ID is copied into the durable successful workspace-registration audit metadata. Source/Git opening has its own source trace. The inspected target does not export source contents, credentials, or prompts.

## HTTP integration

### Runtime receiver and routes

The domain `WorkspaceManagement` interface is declared at `internal/domain/workspaces.go:55-59` (ID `4d0448...`) with methods `FolderRoots` (`d1dffa...`), `Folders` (`8bf776...`), and `AddWorkspace` (`b467ff...`). The implementations endpoint returned exactly one candidate for each method, the pointer receiver `*WorkspaceService`, but each relationship used conditional `signature_match_with_unavailable_types` evidence. The `managedExplorerServer` assignment described earlier directly establishes that production HTTP calls are wired to this service.

`mountRepositories` (`internal/transport/http/repositories.go:20-39`, ID `7866f...`) selects the managed workspace routes when `WorkspaceRuntime` is configured. `mountManagedWorkspaces` (`internal/transport/http/workspaces.go:22-41`, ID `04a3e...`) registers, under `/api` and behind the Explorer authentication middleware:

- `GET /repositories`
- `POST /repositories`
- `GET /workspace-roots`
- `GET /workspace-folders`
- repository-scoped routes mounted by the same helper

The managed server passes both the service as `WorkspaceManagement` and its runtime as `WorkspaceRuntime` (`cmd/server/workspace_runtime.go:29-45`). It marks management enabled when at least one root path exists.

The authentication middleware (`internal/transport/http/auth.go:14-28`, ID `7c5207...`) returns 503 `not_configured` when neither normal authentication nor management is enabled, or when the configured server token is shorter than 24 characters. Invalid bearer authentication yields `WWW-Authenticate: Bearer` and 401. A valid request records trace actor type `operator` before invoking the handler.

### Browse handlers

The roots handler (`internal/transport/http/workspaces.go:65-68`, ID `e9eb...`) calls the interface `FolderRoots` and responds with its result. The folders handler (`workspaces.go:70-82`, ID `e508...`) parses a `folderQuery`, invokes the interface `Folders`, and sends service errors through the shared workspace error mapper.

`folderQuery` (`workspaces.go:84-102`, ID `7453...`) parses the raw query, requires every key to have exactly one value, rejects keys other than `root_id`, `path`, and `offset`, defaults offset to zero, and uses integer parsing when supplied. Parse failures become invalid input. Missing path is rejected by `validFolderPath`; a missing root ID reaches the lookup and becomes forbidden.

### Registration handler

The add handler (`internal/transport/http/workspaces.go:104-129`, ID `b337...`) first enforces same-origin policy, returning 403 `cross_origin_forbidden` on failure. `workspaceBody` (`workspaces.go:131-148`, ID `2664...`) requires JSON, caps the body at 8,192 bytes, disallows unknown fields, requires exactly one JSON value, and requires `root_id` and `path`. It invokes `WorkspaceManagement.AddWorkspace`, then calls `WorkspaceRuntime.Explorer(result.ID)` to initialize repository services. That call occurs after application persistence; if it fails, the HTTP request returns an error without rolling back the already completed registration. The handler returns 201 for a newly added workspace and 200 when `AlreadyAdded` is true.

The comment in this handler says service initialization does not start inference. I inspected the call ordering but did not establish every downstream implementation detail of `WorkspaceRuntime.Explorer`; no stronger inference claim is made here.

`writeWorkspaceError` (`workspaces.go:150-161`, ID `905f...`) maps:

- `ErrNotGitRepository` to 422 `not_git_repository`;
- `ErrFolderForbidden` to 403 `folder_forbidden`;
- `ErrWorkspaceLimit` to 409 `workspace_limit`.

Other errors use the common mapper `errorStatus` (`internal/transport/http/explorer.go:118-140`, ID `c441d...`), which includes `ErrInvalidInput` as 400 `invalid_request`, not-configured as 503, deadline exceeded as 504, and otherwise 503 unavailable. A plain canceled context is not separately mapped and therefore reaches the default unavailable mapping.

The OpenAPI source (`internal/transport/http/openapi_routes.go:5-16`, ID `73d461...`) describes POST registration as persisting and monitoring an existing Git worktree, with a 32-workspace maximum and idempotent duplicates. It explicitly says the endpoint does not initialize or clone Git and does not wait for indexing.

## Callers, callees, and analysis distinctions

The resolved incoming-call endpoint found one production caller of `NewWorkspaceService`: `managedExplorerServer` at `cmd/server/workspace_runtime.go:31`. Public service methods had empty resolved incoming lists. That does **not** mean they are unused: HTTP invokes them through `WorkspaceManagement`, interface dispatch is a documented analysis limitation, and tests are excluded from deterministic typed call selection.

Relevant inspected local callees include `canonicalWorkspacePath`, `validFolderPath`, `root`, `skipFolderEntries`, `readFolderPage`, `selectedPath`, `gitRegistrationError`, `(*Workspaces).Add`, `NewManager`/manager initialization, `persistAddedWorkspace`, and the PostgreSQL save/audit chain. Package-level `source.Open` was also resolved and inspected. Builtins and standard-library calls such as hashing, filepath operations, `os.OpenRoot`, `ReadDir`, and context methods either appeared as builtin/external/unresolved edges or had unavailable bodies; this report preserves that distinction rather than treating absence of a local body as behavior proof.

For interface calls, the conditional implementation candidates were supplemented with construction evidence:

- `WorkspaceManagement.*` candidate: `*WorkspaceService`; runtime assignment in `managedExplorerServer` proves this production receiver.
- `WorkspaceStore.SaveWorkspaces` candidate: `*postgres.Store`; `startExplorer` through `NewPersistentWorkspaces`/`NewWorkspaces` proves this production receiver.

No runtime claim is made for alternate binaries, tests, or configurations that bypass those constructors.

## Test-source evidence (not executed)

I inspected relevant test source through the snapshot API. These are source assertions and fixtures only; no test, build, Git command, or database operation was run.

Application-level evidence in `internal/application/workspace_folders_test.go` includes:

- `TestWorkspaceFoldersRejectEscapesAndNonGitWithoutInitializingGit` (`fdc745...`, lines 25-45): source covers a symlink escape, `..`, `/etc`, an escape path, a missing path, a non-Git selection, and asserts that registration does not create `.git`.
- `TestWorkspaceFoldersBoundDirectoryReadsAndHideGitMetadata` (`cc696...`, lines 47-61), with `collectFolderNames` (`9d019...`, lines 63-81): source creates 105 directories plus `.git`, follows `NextOffset`, asserts page size at most 100, collects all 105 directories, and excludes `.git`.
- `TestWorkspaceFolderValidation` (`6d0d...`, lines 83-98): source covers offsets -1 and 100001, an unknown root, and canceled browsing.
- `TestWorkspaceRegistrationCannotPublishAnUnconfiguredManager` (`84c75...`, lines 100-105): source covers failure without publishing an unconfigured manager.

The integration fixture `managedFixture` (`internal/transport/http/workspaces_integration_test.go:44-65`, ID `10a767...`) is written to use a real PostgreSQL store, persistent workspaces, the workspace service, and an `httptest` server. The main managed-workspace persistence test (`c06d16...`, lines 67-96) covers 201 creation, 200 duplicate registration with the same ID and `AlreadyAdded`, persistence across group recreation, audit data, watch updates, and an unavailable saved path. Supporting source checks authentication and validation (`a6209...`, lines 144-159), queries audit actor `operator`, nonempty trace ID, and completed outcome (`61a025...`, lines 161-168), and rejects malformed/oversize JSON and cross-origin registration (`f053...`, lines 170-186).

Config test source `TestWorkspaceRootsRequireBoundedAbsolutePathsAndAuthentication` (`internal/config/repositories_test.go:113-131`) covers a valid root and rejects object-shaped JSON, relative roots, null, NUL, a 4,097-byte root, and missing API authentication.

The snapshot details explicitly state that test variants were excluded from deterministic typed call selection. Test-source presence is therefore not evidence that the snapshot builds or that the tests pass.

## Errors and observable outcomes summary

| Condition | Service error | Managed HTTP outcome |
|---|---|---|
| Empty/nonlocal/NUL/overlong folder path; offset below 0 or above 100000; name over 200 bytes or with NUL | `ErrInvalidInput` | 400 `invalid_request` |
| Unknown root, resolved escape, missing selection during resolution, folder/root open failure, or registration source-root mismatch | `ErrFolderForbidden` | 403 `folder_forbidden` |
| Selected existing path is not exactly an existing Git worktree root | `ErrNotGitRepository` unless context is already errored | 422 `not_git_repository` |
| 32 unique managed workspaces already exist | `ErrWorkspaceLimit` | 409 `workspace_limit` |
| Duplicate canonical repository path | success with `AlreadyAdded=true` | 200 |
| New durable registration and runtime initialization succeed | success with `AlreadyAdded=false` | 201 |
| Deadline exceeded | context deadline error | 504 |
| Plain cancellation or otherwise unmapped internal failure | propagated/default error | 503 unavailable |

Body parsing and same-origin rejection happen before the service and have their own transport errors. An HTTP runtime-initialization failure happens after registration persistence and can report failure even though the workspace is already durable.

## Remaining uncertainties and non-claims

- The repository was not built and tests were not run. Syntax-level completeness does not establish compilability.
- Standard-library and Git executable behavior was not inspected beyond the application's calls and returned source wrapper. Actual path semantics, permissions, concurrent filesystem changes, Git availability, and database availability are runtime facts.
- `os.Root` confinement is application intent supported by the selected API and source comment, but its unavailable platform implementation was not re-proved here.
- Implementation records carrying `signature_match_with_unavailable_types` are conditional. Production construction was inspected for the two relevant receivers, but no claim is made for every possible runtime configuration.
- Empty incoming-call lists for public methods do not prove absence of use. Interface and test calls are analysis gaps.
- The target and its inspected production path show no model invocation, upload, repository initialization, or wait-for-index behavior. This is a control-flow statement, not a claim about unrelated repository features.
- Repository/catalog upsert and workspace-registration persistence do not share one transaction. The precise persisted state after injected database failures was not exercised.
- Snapshot metadata says the source tree was dirty, no commit was recorded, and changes are not known. All conclusions are pinned to the snapshot ID, not to a Git revision.

## Request ledger and procedural compliance

The companion JSON ledger contains 89 HTTP attempts, each with method, route, status or failure, raw response-body byte count, and elapsed milliseconds. One initial GET failed in the sandbox before a response was received and was retried successfully through the permitted local API access. One deliberate lookup of a nonexistent test path returned the expected 404. The other recorded API retrievals completed successfully. The bearer credential is omitted.

No isolation violation occurred. The only filesystem writes were the two allowed final artifacts in `/private/tmp`.
