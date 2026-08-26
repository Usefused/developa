# Multiple repositories

One engine can watch up to 32 operator-configured Git checkouts. Each has an independent tracker, published snapshots, source index, feature jobs, AI cache and audit scope in the same PostgreSQL database. Adding repositories does not enable automatic inference.

## Configuration

For a native server, replace `REPOSITORY_PATH` with a JSON array:

```sh
unset REPOSITORY_PATH REPOSITORY_NAME
export REPOSITORIES='[{"name":"API","path":"/absolute/checkouts/api"},{"name":"Worker","path":"/absolute/checkouts/worker"}]'
# Keep DATABASE_URL and a random DEVELOPA_API_TOKEN (at least 24 bytes) configured.
./bin/server
```

`REPOSITORY_PATH`/`REPOSITORY_NAME` remain a single-repository shorthand. Do not combine them with `REPOSITORIES`. Names are optional; they default to the checkout directory name. Multi-repository paths must be absolute. These settings seed PostgreSQL registrations at startup. Invalid or duplicate seed checkouts fail startup. Already saved workspaces restore without repeating environment entries; if a saved folder becomes unavailable, its snapshots remain readable and tracker status explains the failure.

Repository IDs derive from canonical server checkout roots. Renaming a display name does not change identity; moving a checkout to a new root creates a different identity. Keep container mount paths stable across restarts. Removing an environment entry does **not** unregister a saved workspace. There is currently no workspace deletion endpoint. Re-adding the same root is idempotent and does not create another tracker.

The API does not clone URLs or accept arbitrary absolute filesystem paths. Operators grant access by configuring and mounting folders. All repositories share the server’s operator token and model configuration; this is not a multi-tenant permission system.

## Add from the filesystem

```sh
export WORKSPACE_ROOTS='["/absolute/checkouts"]'
# REPOSITORIES may be empty: unlock the UI, then choose Add workspace.
```

`WORKSPACE_ROOTS` is a JSON array of up to 16 absolute engine directories. If omitted, the picker is limited to the explicitly configured checkout roots, never their parents. It does not constrain existing saved registrations. Use a narrow directory containing only repositories you intend this operator token to access. In Docker mount the parent folder read-only and set the container path, such as `["/repositories"]`.

The dialog browses folders without uploading files. Select the Git working-tree root and optionally enter a display name. Non-Git folders produce a clear error; the server never runs `git init`. Registration, audit and outbox records commit before monitoring begins. Up to 32 workspaces are supported. Watchers resume on startup; directory browsing does not invoke Ollama.

For agents, use these authenticated, small calls:

1. `GET /api/workspace-roots` → `[{id,name,path}]`. Paths are visible only to the authenticated operator.
2. `GET /api/workspace-folders?root_id=ID&path=.&offset=0` → `{root_id,path,items:[{name,path}],next_offset}`. Paths are relative to the selected root; browse using returned paths. Each page reads at most 100 directory entries, skips files, `.git` and symlink entries, and follows native directory order. Continue even if a page has no folders but has `next_offset`; changing directories may change page ordering. Offsets are bounded to 100,000.
3. `POST /api/repositories` with `{"root_id":"ID","path":"project","name":"Project"}` → HTTP 201 `{id,name,already_added:false}`, or HTTP 200 with `already_added:true` for an existing registration. JSON is limited to 8 KiB, names to 200 bytes. Unknown fields, traversal paths, and cross-origin browser writes are rejected. A folder outside the allowlist returns 403 `folder_forbidden`; a non-Git/non-root folder returns 422 `not_git_repository`; capacity returns 409 `workspace_limit`.
4. Poll `/api/repositories/{id}/project` for the first published snapshot. Registration does not wait for parsing or inference.

## Docker mounts

Set `REPOSITORIES` in `.env` using **container paths**, leave `REPOSITORY_PATH` empty, and supply a custom Compose override such as:

```yaml
services:
  server:
    volumes:
      - type: bind
        source: /absolute/checkouts/api
        target: /repositories/api
        read_only: true
        bind:
          create_host_path: false
      - type: bind
        source: /absolute/checkouts/worker
        target: /repositories/worker
        read_only: true
        bind:
          create_host_path: false
```

```dotenv
REPOSITORIES='[{"name":"API","path":"/repositories/api"},{"name":"Worker","path":"/repositories/worker"}]'
REPOSITORY_PATH=
```

Run `docker compose -f compose.yaml -f your-repositories.yaml up --build`. Do not combine this with `compose.repository.yaml`, which sets the single-checkout path.

## API discovery and scope

1. `GET /api/repositories?limit=24&offset=0` returns configured repositories as `{items:[{id,name,snapshot?}],total,limit,offset,default_repository_id}`. Optional `q` searches names with a literal case-insensitive substring. Filtering, ordering, pagination and totals use one SQL statement. The endpoint never returns server checkout paths.
2. When the caller already knows an absolute repository root visible to the engine, `POST /api/repositories/resolve` with `{"path":"/repositories/api"}` returns that repository identity and latest snapshot. Matching is exact after symlink canonicalization; nested folders and unregistered roots return `404`. The response never echoes the path and the request invokes no Git command, indexing, or inference. A Docker engine sees mounted container paths, not the caller’s host path.
3. `GET /api/repositories/{id}/project` returns that tracker’s status and latest snapshot.
4. Use that ID and snapshot for source reads, for example `GET /api/repositories/{id}/snapshots/{snapshot}/symbols?q=Handle`.
5. The same prefix supports every existing API: files, details, calls, chains, flow, context, features, jobs/events, answers and function reviews. `POST /api/repositories/{id}/scan` affects only that repository.

All requests require the operator Bearer token. Unknown/unconfigured IDs and records outside the selected repository return 404. A snapshot ID from another repository cannot widen the scope. Short legacy `/api/project` and `/api/snapshots/...` routes select the first configured repository; agents should use explicit IDs so configuration ordering cannot change their target.

## Browser and background work

Use the searchable **Workspace** dropdown in the header. Navigation URLs carry `repo` alongside the snapshot. Switching retains the sidebar page but removes function, feature, filter and old snapshot selections. Read caches are isolated, obsolete UI requests/streams are canceled, and editor mappings are saved per repository. Already accepted background jobs continue on the server. The verified API key is saved in localStorage for refreshes; Lock workspace or any authentication rejection removes it.

Structural watchers run independently without AI. Feature workers keep separate durable queues but share one background execution slot per server, acquired before the lease. Interactive explanations are still explicit and separately gated. A failure in one running tracker preserves its last snapshot and does not stop the other watchers. There is no combined cross-repository call graph or dependency resolution in this version.
