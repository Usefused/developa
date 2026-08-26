# Multiple repositories

One Denverr process can watch up to 32 operator-approved Git checkouts. Each repository has an independent tracker, immutable snapshots, source index, feature jobs, AI cache and audit scope in the same PostgreSQL database. Adding a repository does not enable automatic inference.

## Native startup

The simplest setup starts Denverr from the common parent of the repositories:

```sh
export DATABASE_URL='postgres://denverr:password@127.0.0.1:5432/denverr?sslmode=disable'
cd /absolute/checkouts
denverr serve
```

When no repository environment is configured, the current directory becomes the allowed workspace root. Use repeatable flags for several separate parents:

```sh
denverr serve \
  --workspace-root /absolute/checkouts \
  --workspace-root /absolute/work
```

The UI's **Add workspace** dialog browses only those roots. Select a Git working-tree root and optionally enter a display name. Non-Git folders return a clear error; Denverr never runs `git init`. Registration, audit and outbox records commit before monitoring begins.

## Persisted registration

Workspace registrations and canonical checkout roots are stored in PostgreSQL and restored at startup. A missing saved checkout does not erase its snapshots; tracker status explains why new indexing cannot continue.

Register a checkout before starting the process:

```sh
export DATABASE_URL='postgres://denverr:password@127.0.0.1:5432/denverr?sslmode=disable'
denverr workspace add --name API /absolute/checkouts/api
denverr serve --workspace-root /absolute/checkouts
```

`workspace add` is idempotent and reports `already_added`. Because it writes directly to PostgreSQL, restart an already running Denverr process so it creates the new in-memory tracker. Registrations made through the HTTP API or UI start their tracker immediately.

Repository IDs derive from canonical checkout roots. Renaming a display name does not change identity; moving a checkout creates a different identity. There is currently no workspace deletion endpoint.

## Environment seeding

Scripted deployments can seed registrations at startup:

```sh
unset REPOSITORY_PATH REPOSITORY_NAME
export REPOSITORIES='[{"name":"API","path":"/absolute/checkouts/api"},{"name":"Worker","path":"/absolute/checkouts/worker"}]'
export WORKSPACE_ROOTS='["/absolute/checkouts"]'
denverr serve
```

`REPOSITORY_PATH` and `REPOSITORY_NAME` remain a single-repository shorthand and cannot be combined with `REPOSITORIES`. Names default to the checkout directory name. Seed paths and workspace roots must be absolute. Invalid or duplicate seeds fail startup; removing an environment seed does not unregister an already persisted workspace.

Use narrow roots containing only repositories intended for anyone who has the shared operator token. The API does not clone URLs or accept a repository-defined model configuration. All workspaces share the process token and model policy; this is not multi-tenant authorization.

## Filesystem browsing API

Agents and the browser can register a folder with these bounded calls:

1. `GET /api/workspace-roots` returns authenticated operator paths as `[{id,name,path}]`.
2. `GET /api/workspace-folders?root_id=ID&path=.&offset=0` returns folders relative to that root. Each page reads at most 100 entries, skips files, `.git` and symlinks, and caps offsets at 100,000.
3. `POST /api/repositories` with `{"root_id":"ID","path":"project","name":"Project"}` returns HTTP 201 for a new registration or HTTP 200 with `already_added:true`.
4. Poll `GET /api/repositories/{id}/project` for the first published snapshot. Registration does not wait for parsing or inference.

Unknown fields, traversal paths, cross-origin browser writes, and folders outside the allowlist are rejected. A folder that is not a Git root returns `422 not_git_repository`; capacity returns `409 workspace_limit`.

## Repository discovery and scope

1. `GET /api/repositories?limit=24&offset=0` returns configured repositories with the default repository ID. Optional `q` searches names in PostgreSQL.
2. `POST /api/repositories/resolve` with `{"path":"/absolute/checkouts/api"}` resolves an exact path visible to the Denverr process. Matching happens after symlink canonicalization. Nested folders and unregistered roots return 404. The path is never echoed and the request invokes no Git, indexing, or inference work.
3. `GET /api/repositories/{id}/project` returns that tracker's state and latest snapshot.
4. Use the returned repository and snapshot IDs for every subsequent read, such as `GET /api/repositories/{id}/snapshots/{snapshot}/symbols?q=Handle`.

Every source, calls, flow, context, feature, job, answer and review endpoint has the same repository prefix. Unknown repositories and snapshots from a different repository return 404. Short `/api/project` and `/api/snapshots/...` paths remain first-workspace compatibility aliases; agents should use explicit repository IDs.

## Browser and background work

The searchable workspace selector keeps `repo` in the URL. Switching repository retains the sidebar page while clearing stale source selections, aborting obsolete requests and streams, and isolating read caches. Editor mappings are stored per repository. Accepted background jobs continue after browser navigation.

Structural watchers run independently without AI. Feature workers keep separate durable PostgreSQL queues and share one background execution slot per process. Interactive explanations remain explicit and separately gated. A tracker failure preserves that repository's last snapshot and does not stop other watchers. Cross-repository call graphs and dependency resolution are not implemented.
