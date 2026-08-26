# Frontend

The browser application uses React Router 7.18 framework mode (the maintained successor to Remix v2), React 19 and Vite. It is built as a SPA: Node is needed for development/builds only. The Go server embeds the generated HTML and assets and owns all API, authentication, indexing and inference work.

## Routes and components

`internal/webui/app/routes.ts` defines `/blocks`, `/flow`, `/changes`, `/analysis`, `/features` and `/chain`. Each sidebar item is a real link with its own route module. File selection, filters, pagination, selected functions/features and traversal settings live in URL parameters. Back/forward navigation restores those selections. `/` opens Code blocks; historical `/?snapshot=…` links still open Features on that exact source.

Route modules compose small components under `app/components/`. Shared hooks manage credentials, snapshot selection, bounded read caching, preferences, request cancellation and model actions. `assets/` contains shared styles, API/protocol helpers and the accessible searchable combobox, not a second application. `flow-source/` contains the React Flow diagram; `code-source/` contains the Go tokenizer. The former global Explorer controller and imperative page renderers have been removed.

## Data and execution rules

- The browser calls the existing Go API. Route rendering never starts inference.
- Reads are snapshot-pinned, cached in memory for two minutes and bounded to 64 cache entries. Explicit refresh bypasses the cache. Routine project polling remains two minutes; initial/manual scans use temporary faster status polling.
- A successfully verified API token is saved under `developa.api-token` in this origin's browser localStorage and restored on refresh. Locking or a 401 deletes it, clears caches and unmounts views, aborting outstanding requests and streams. Tokens never enter URLs. Disabled browser storage falls back to the current in-memory session. This is plaintext browser storage: use a trusted browser/profile and keep this origin free of untrusted scripts.
- Add workspace browses authenticated, operator-allowed engine folders. The API validates Git before persisting registration and starting its watcher. Browser-local uploads are not used, because the engine needs a stable filesystem checkout to monitor.
- Navigation aborts obsolete reads, and already-decoded late responses cannot replace newer data.
- Feature progress subscribes to read-only SSE in its own component. It never refreshes or remounts the saved feature cards. Users load new saved results with **Refresh saved features**.
- AI explanations and batched reviews run only from explicit buttons, using the existing audited API endpoints. Failed POST streams are never automatically replayed. Accepted background feature jobs continue on the server after navigation.
- AI explanation sections show only the action button until content exists. A read-only `answers/lookup` restores persisted explanations without contacting Ollama; the request body keeps the question out of URLs. Successful generation also updates the scoped memory cache. Saved content remains visible after reopening/restart and requires fresh generation only when its question or source context no longer matches. Existing explanations disable repeat generation in the UI.
- All selectors are searchable except traversal depth, which remains a native select.
- Main content uses the available width with 16px side gutters (12px on phones). Desktop navigation stays pinned beneath the header and scrolls independently on short screens. Symbol/feature detail panels have a viewport-bounded scroll area that contains wheel/touch scrolling at either boundary. The page retains native document scrolling and route scroll restoration.
- The top Workspace selector searches the configured repository catalog through bounded API calls. `repo` in the URL scopes every API read, scan, feature job and explanation. Switching retains the sidebar page but clears source selections, replaces the private read cache and aborts old UI requests/streams. Browser back/forward restores repository-qualified URLs. Existing links without `repo` use the server’s first configured repository.
- Editor checkout roots and editor selection are stored per repository; the theme remains shared. No checkout path is sent to the server by these settings.
- Editor links use the desktop editor’s native URL scheme. The browser/OS must permit opening external applications; the web app cannot confirm that handoff succeeded. Function and citation links include an **Editor didn’t open?** fallback with the exact local file location and an editor settings shortcut. No server-side process launch or automatic retry is used. VS Code documents its URL format at https://code.visualstudio.com/docs/configure/command-line#_opening-vs-code-with-urls.

## Development and deployment

Run the Go API on `127.0.0.1:18089`, then `npm run dev`. Vite proxies `/api` to that server; no secrets belong in Vite config or client environment variables. The dev server uses framework development tooling; production uses the Go server's stricter security headers.

`npm run build` produces `internal/webui/dist/`, an embedded generated artifact. Do not edit it by hand. `npm run build:check` rebuilds and checks the embedded assets. `make build` builds the frontend before compiling the Go binaries. Docker's Node build stage performs the same frontend build; the final container still runs only Go.

The static shell contains nonce placeholders. Go generates a fresh CSP nonce for each HTML response and injects it into framework hydration and scroll-restoration scripts. Inline handlers, arbitrary inline scripts, remote scripts and embedding remain blocked. Only known UI routes receive the shell; missing API routes and assets stay 404.

## Verification

`npm test` runs API/SSE, graph, source and selector tests plus React route/component tests in jsdom. The latter exercise cached navigation, SSE card identity, explicit refresh and generation failures, canceled reads/actions, feature switching, native depth and safe source rendering. `npm run lint` enforces the ten-branch complexity ceiling for handwritten JS/JSX, including tests. Go tests cover embedded assets, deep links, fresh CSP nonces and separation from API routing.
