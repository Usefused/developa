# Blind API-only report: AI-Enabled Explorer Server

Date: 2026-08-26

## Scope and snapshot provenance

This investigation used only HTTP responses from `http://127.0.0.1:18089`. It did not read workspace files, Git data, process state, the database, documentation, terminal history, Ollama, or any inference endpoint.

API discovery identified repository `cd106ad658f4cd3b9b86829ca9fef7f62d8be9fb75e0f366755d66aafa197732` (`Developa`). Its latest snapshot was `450a79128f52e6989bcdfd30b0a4afd3bc7ff62d11baa00f2e8b03b726e32df7` (index v4; 237 files; 3,377 symbols). Searching features there returned a saved-snapshot pointer to `9f4c35dd2def37ee721a7e0b95fa3ab4aec1bc635cc64fbd35b0acbc6bea194f` (index v2; 174 files; 2,612 symbols), so the feature was investigated on that saved snapshot.

Feature `acaf4314c4b74af2e29e39e5f581260563d0c07661ab72005e4651dbbac91ddf`, **AI-Enabled Explorer Server**, is explicitly `inferred`. Its evidence is `explorerServer`, symbol `0ee11c6314ca7ec2746cf1b851106a267da907cd775caab73f2fca1986f47c9e`, `cmd/server/intelligence.go:15-35`. Saved feature run `1e43999c-782a-45cb-8c63-e026fd904ab7` was partial: 129/2,612 symbols, 112 features, 19 cached batches and one model call. Its stored limitations say inferred features are not proof of runtime behavior, coverage is partial, and bounded batches do not establish cross-batch behavior.

## Source-proven facts

### Initialization and service wiring

`main` calls `run`. `run` loads configuration; creates a signal-cancelled context; initializes telemetry; connects PostgreSQL; starts the repository manager; constructs the explorer server and feature worker; starts the worker; and serves until shutdown, with store/manager/worker/telemetry cleanup. Evidence: `run`, `69493ab1f4678061fb7601d0fd0471adbab40073705220f7ad8f295f913cb6ec`, `cmd/server/main.go:28-57`; `main`, `925ffca8340f50217118106849508de4087103a12ae9aa4d3bce397f59de6822`, `cmd/server/main.go:21-26`.

`startExplorer` runs catalog migrations, creates `application.Manager` with configured repository path/name, watch interval and scan timeout, then starts it. Evidence: `9c021806498d8c1d7c77da0159c48f303053186a7122e9978ffa0738df3178e8`, `cmd/server/main.go:59-74`. `NewManager` and `initialize` open/register only the configured checkout and load its latest snapshot. Evidence: `df0e730ea8fa82a4d30cdb587df88e9677c68cc9336a4e4857570650366631a2`, `internal/application/manager.go:58-76`; `b0cc7715ef7032ce3857beaf95c92ccb9d5bc16060d30d0d72900f6c8e8739b5`, `internal/application/manager.go:78-102`.

`explorerServer` obtains the manager repository ID and builds two intelligence services: one with `OllamaAnswerModel`, one with `OllamaAnalysisModel`. The analysis service feeds `AnalysisWorker` and, if it implements `FunctionReviewer`, also handles review generation. The HTTP `Explorer` receives PostgreSQL as catalog/knowledge/job store, Manager as tracker, answer intelligence, reviewer, worker, repository ID, API token, cloud flag, and automatic-feature flag. Evidence: `explorerServer`, `0ee11c6314ca7ec2746cf1b851106a267da907cd775caab73f2fca1986f47c9e`, `cmd/server/intelligence.go:15-35`.

`intelligenceService` creates an Ollama client only when a model name exists, then calls `NewIntelligence` with repository scope, AI timeout and `MaxModelCalls: 1`, making one model batch a durable checkpoint. Evidence: `ecc070a0c7343852ebb6f7d1af38a6639b0d6cc2e52d05348440d8ede215ead6`, `cmd/server/intelligence.go:46-60`. `analysisWorker` disables its intelligence when `AIIndexEnabled` is false. Evidence: `08b3a884dc785e222fd498b95b314ab8fa38ba9e7bc32e6edb0d6e682cc0f684`, `cmd/server/intelligence.go:37-44`.

`NewServer` sets a five-second header timeout, configured read timeout, 60-second idle timeout, 16 KiB maximum headers, and a write timeout that reserves six seconds beyond the larger request/AI timeout. Evidence: `b3532bc551e470d022d679d2cd0df2fcc158a89271831850c474a180acd62f37`, `internal/transport/http/server.go:25-36`.

### HTTP surface

`NewHandler` installs tracing, panic recovery and route-sensitive timeouts; exposes `/healthz` and `/readyz`; mounts Explorer and the embedded UI; and returns bounded not-found/method-not-allowed errors. Evidence: `5e1e2a8af68b397be5a064d37acb2ead9e36a6bfe87b5ffda2d13698dde2922a`, `internal/transport/http/server.go:38-54`.

`mountExplorer` makes `GET /api/info` public, authenticates the rest of `/api`, and exposes project status, manual scan, capabilities, snapshot file/symbol search and detail, and snapshot details. Evidence: `9629453963a90444d10da4c6957f40ae8cb183d93a9d4fa16750ca592cf8f8eb`, `internal/transport/http/explorer.go:27-44`.

`mountKnowledge` adds:

- `POST /features/generate`, `GET /analysis-job`, `GET /events`;
- `GET /calls`, `/flow`, `/symbols/{symbol}/chain`, `/context`, `/features`, `/features/{feature}`;
- `POST /answers`, `/answers/stream`;
- `GET/POST /function-reviews`, `POST /function-reviews/stream`.

Evidence: `mountKnowledge`, `a5b1b6d93b2acc696d64e3774efdd66a7a369ec1d615198fd4aa1f92feacf3d6`, `internal/transport/http/intelligence.go:10-28`. The UI serves `/` plus embedded assets: `Handler`, `fbbf2bb0a80a93b9d5f505e35d0afe98eb8bcbd3483e4a8e7a48ef7d6ff1aab8`, `internal/webui/webui.go:13-26`.

The saved source does not mount answer lookup. The current OpenAPI includes a newer `/answers/lookup`, so it is not attributed to the older feature snapshot.

### Automatic versus request-only behavior

`Manager.Start` launches a background loop. That loop performs a system `startup` scan, accepts manual requests, and performs system `watch` scans on its poll timer; it resets the timer after every scan to avoid starving manual requests. Evidence: `a45e29eebeaed5f81b9c46bc82ced530244c7eafaf8b540b6ff2aab94c233d05`, `internal/application/manager.go:122-134`; `Manager.run`, `5a244cf29f8757e53c4a14be7b0930210d40528dbc9c0774429549196aaad71c`, `internal/application/manager_queue.go:65-86`. `POST /api/scan` is the manual alternative.

The feature worker starts automatically but is a no-op unless available. It reconciles durable work once, then repeatedly claims/processes one job and waits a configured interval, explicitly bounding background model traffic. Evidence: `AnalysisWorker.Start`, `4b4748c523f3570bebf079f7f76796f7415d809336c6b94092bda27cb220eb76`, `internal/application/analysisjobs.go:105-115`; `AnalysisWorker.run`, `a69d4076d9c302914c6ffffbcf19e0042f4527f8341c5a51ed417addb2f71ef1`, `internal/application/analysisjobs_run.go:18-34`; `waitAnalysisPoll`, `2cc89727d62661448f2a68bc9a5511e9a48ac88b216622e3f8dcee98b5857422`, `internal/application/analysisjobs_run.go:36-46`.

Automatic feature jobs require separate opt-in. `connectDatabase` enables them only if AI indexing, automatic features and an analysis model are all configured. Evidence: `d47108007c8e8de0870d7833a092655769f2e8877ccbf51590b03bc9f1390d8c`, `cmd/server/main.go:76-91`. Defaults are `AI_INDEX_ENABLED=true`, `AI_AUTO_FEATURES=false`: `600a8adc1996ff4d7b0a48ac842b714404e25e1c186977df1a551ecb042a7f7e`, `internal/config/intelligence.go:41-56`. Startup reconciliation is actor `system`, trigger `feature_startup`: `30e7b17f028622e1615ddd7dc0e2caad2789ef8e316f4093bf6374b64241e8fa`, `internal/application/analysisjobs_run.go:48-60`.

Manual feature generation is request-only. `generateFeatures` validates mutation/body and worker availability, queues the route snapshot, and returns 202. Evidence: `abea7cdea4250f61164dd595c914b25471f8ffcca3837c956ae14724131ace11`, `internal/transport/http/intelligence.go:118-136`. `Queue` records actor `operator`, trigger `feature_manual`: `1ddd3d832cefaed67c1b2fcaac94cb4c5965a1f71be0659422aed560c594a2f6`, `internal/application/analysisjobs.go:76-92`. Answers and review generation run only on their POST routes; reads/status/events do not initiate them.

Current runtime capabilities reported calls/flows/context/features/function reviews available, but answers, analysis jobs, automatic features, Ollama and review generation unavailable. This describes current configuration, not the saved run's historical configuration.

### Feature generation and persistence

The worker claims a leased job, checks whether it is current/superseded or already published, and invokes `Discover` only when work remains. Evidence: `processNext`, `9691a8c4a4cbbc25de43a9263cf0284bd83de98d66f7e330a9cfb08db7703883`, `internal/application/analysisjobs_run.go:62-73`; `analyze`, `ac66ebfdb1f5192f4c7432a4871d4e9a92b604cd937b01925293f4656280537e`, `internal/application/analysisjobs_progress.go:13-38`.

`Discover` starts an audited execution, reconstructs resumable state, performs bounded discovery, finalizes status/limitations and calls `SaveFeatures`. Evidence: `15eaf1e19a5f48d65f66e517b1385bd7a7406d7ba0ee64c61bce4ac3a7bbc862`, `internal/application/intelligence_features.go:32-52`. Resume pins parent run, counts and compatible model identity: `featureState`, `796ea1983850638abcf07e16e83821f1ee6509778ab78e66c87b34199b1fef22`, `internal/application/intelligence_resume.go:14-32`.

`discoverBatches` pages immutable snapshot symbols and advances only by facts actually used. Evidence: `20739a78485223726c6e68d46284384de5f143337c6f79793fdb5d2fe663de153d0418c6e4b8f29dc14df15cf753a9b3`, `internal/application/intelligence_features.go:54-77`. `discoverBatch` creates byte-bounded evidence, checks an exact-input cache or invokes the model, validates features, pins model identity, saves validated cache data and appends results. Evidence: `583b3b162e8b048160e39e4b50806bb977bc5576ecac365f601f083a295a8676`, `internal/application/intelligence_features.go:86-120`; `boundedEvidence`, `8e03ce1d806dd4ec0bbb1039c0b809655f30c1f476f428452d97c4fa7922db8b`, `internal/application/intelligence_context.go:34-64`.

Model output is untrusted. `validateFeatures` permits at most eight features per batch, bounds title/summary, requires citations canonicalized from supplied facts, derives feature IDs, and marks them `inferred`. Evidence: `aaad8e3a6a0bec84a1b9aa5910cf1d860c6b1e8b2474a668fa35230126cce394`, `internal/application/intelligence_features.go:122-145`; `canonicalEvidence`, `949bd5a2d791c872fd2131445fd7013a74719b6495e2bc7ccc01e0a79496d485`, `internal/application/intelligence_context.go:107-130`.

Discovery reserves up to five seconds to atomically publish completed partial progress. Partial coverage, budget exhaustion and source truncation are recorded as limitations. Evidence: `discoverBudgeted`, `b5bf5e00f24c8b8617c580205053aac447c82ab2a931ccf0e3b2d34cc2e0cd7b`, `internal/application/intelligence_resume.go:45-61`; `finishFeatureRun`, `2d432c6d79e429bf75ca8fd6c64d8c29ccda1632764ac438d5ec672a1c899ddf`, `internal/application/intelligence_features.go:147-161`.

`SaveFeatures` validates and uses `intelligenceWrite`. That transaction locks the repository/snapshot row, validates the analysis lease before and after mutation, inserts features, appends a durable audit event, commits, then emits `intelligence.published`. Evidence: `2827f98e0d0008d78371ed67fad086af56a566841be6e77839b119929762a269`, `internal/store/postgres/intelligence_write.go:69-77`; `intelligenceWrite`, `c4364fce08a617b670435317ae4736517648730134976e167e0730915b33ad0c`, `internal/store/postgres/intelligence_write.go:21-56`.

### Answer persistence

The HTTP handler validates same-origin mutation, parses the request, checks answer-service availability, and invokes `Intelligence.Answer` for the route snapshot. Evidence: `answer`, `7cbd73eb454b2e44991a5f3e0f8369106c50ef770e0287112ccf8f0d236c2026`, `internal/transport/http/intelligence.go:148-155`; `prepareAnswer`, `ad0ba83f2ac185195875f53efbc5ef27e8509b16499860a7d18d84881b9533ba`, `internal/transport/http/intelligence.go:157-171`.

`Answer` begins an audited execution, retrieves evidence scoped to snapshot and optional symbol/feature/flow, generates/validates the answer, marks completion and calls `SaveAnswer`. Evidence: `032b443d649bdb881dca6134c4ad4e00b6c458dc3d77dc485a2391a5f821fe8f`, `internal/application/intelligence_answer.go:16-39`. Feature questions load only the feature and bounded feature context: `47cf858871db16e0f48c04d8c2e14b2e66bf21f56be9cb0c003dcdb56261a588`, `internal/application/intelligence_answer_context.go:81-97`.

With no facts, `generateAnswer` returns an explicit insufficient-evidence answer without a model call. Otherwise it uses cache/model, records identity/cache state, validates schema/text/canonical evidence and saves validated cache output. Evidence: `6991cba9005e3f688dd007ca1c8d50d0ef3908e62801774b41e0c7fb4273a215`, `internal/application/intelligence_answer.go:41-66`; `e537a6697fa36902d46d075f0a019536aec11954c3b40c8af3bba20adba4b40a`, `internal/application/intelligence_answer.go:68-82`; `e0c2df07010e6c388d8acd8859f566313ba4320587b34f2bf2859b301be9bf90`, `internal/application/intelligence_answer.go:84-114`.

`SaveAnswer` rejects invalid model output and uses the same transactional audited publication, recording one answer and citation count. Evidence: `b621e80f3f505f1f1e04246d2e3864f93ecc436c06802f7a18a65a55bac31f00`, `internal/store/postgres/intelligence_write.go:240-248`. Streaming calls the same persistent service: `streamAnswer`, `cb91d55f6fa4d1b8840110397da933fbe34764430db6b76328bb90efecdc1b2e`, `internal/transport/http/events.go:188-191`.

### Reliability, audit, privacy and security boundaries

- **Authentication and repository scope:** all `/api` except `/api/info` requires a configured token of at least 24 characters. Exactly one Bearer header is accepted; SHA-256 digests are compared in constant time. Evidence: `authenticate`, `7c52071fd7fbd48c95fd3cef93ab4ae9f54c7cebe6f9c58a6afdecb5b82d65be`, `internal/transport/http/auth.go:14-28`; `authorized`, `03f54817c1c285944b31771eeb0908025fe515f3ab7e27dcf8b291ad03893e17`, `internal/transport/http/auth.go:30-41`. `Explorer` is fixed to one operator-configured repository: `af40825d0109e6ba9f1f6e9610a06a6cd6dea66a9834e6ff53e149425a241e14`, `internal/transport/http/explorer.go:14-25`.
- **Cross-origin mutation protection:** AI mutations reject cross-site `Sec-Fetch-Site`, multiple Origins and mismatched Origin; no Origin remains valid for CLI. Evidence: `validMutation`, `9bd3bcd5d71d332a0c0548379575edfc94a15686664a3b54aa7eef3850082eca`, `internal/transport/http/intelligence.go:173-179`; `sameOrigin`, `81213a75c3593eedbdea2e4aadb12d92eaea8d31e99b5dce0208a70158452690`, `internal/transport/http/auth.go:43-55`.
- **Concurrency/timeouts:** `IntelligenceService.begin` uses a one-slot gate, returns busy on overlap, applies timeout, creates execution/trace identity, and durably records `running` before work. Evidence: `078e8caa29d82cdbe163b69c470e42a2f4da5ef1a1de00882fcf2516fc0077e1`, `internal/application/intelligence.go:84-112`. `executionTimeout` gives longer limits only to model/stream routes: `2938c13bebde010d3d4661460dfcdc14b2c4a9bad3d1e845e8f7ff3db338de6b`, `internal/transport/http/timeout.go:12-34`.
- **Lease fencing/retry:** worker publication validates leases; lost leases do not publish; shutdown gets a bounded non-cancelled finish window. Evidence: `process`, `ec99906c0ec5a333203b15bc6ecf88f379ac5d40e0c6cba697d2f0f5b26c8bf4`, `internal/application/analysisjobs_run.go:75-107`. SQL claim uses `FOR UPDATE SKIP LOCKED` and bounded retry: `claimAnalysisSQL`, `c1e86df92f4254d9db8c315dd0322716cdddf207c3cf5bd17d037c8d8976e458`, `internal/store/postgres/analysis_job_queries.go:57-67`. The saved job ended `failed` after 3 attempts/20 chunks with `invalid_model_output`; its previously published partial run remains queryable.
- **Durable audit:** successful mutation and audit commit atomically. Audit stores IDs, actor, trigger, trace, outcome, counts and job ID, not prompt/source. Evidence: `appendAudit`, `e33d42de88afae5bf9470e3fea5fc12c7bad14fee1a0f41943be8fe07745b889`, `internal/store/postgres/audit.go:71-83`. Failed/cancelled executions receive a five-second non-cancelled audit attempt: `IntelligenceService.end`, `96ec5726b338c1a06554558e021d17350f67a4cf4886cff6c186182c76310587`, `internal/application/intelligence.go:114-134`.
- **Trace/error privacy:** traces propagate context and return `X-Trace-ID`; only route templates/status are exported, not raw URLs. Evidence: `traceRequest`, `d4514bfbe3c5e3f468201daa0e5b79549369107d44fac3161264ec31a19cca17`, `internal/transport/http/tracing.go:16-29`; `finishRequest`, `78562a204a333cde462bf626d6a3c50ee54c90293937e1d2ee28f21cc0fa4ac9`, `internal/transport/http/tracing.go:31-44`. Panic payloads are not logged because they can contain source/credentials: `recoverRequest`, `3d66cc4b175f451b14ad8f4b5c7812f92986478283ef91b3526e8000b51eb644`, `internal/transport/http/tracing.go:46-57`.
- **Prompt/output boundary:** `groundingInstructions` treats source/comments/names/question as untrusted data, denies tools/embedded instructions, and allows only supplied symbol IDs. Evidence: `d96295479e9eb9711f469633c783466beb2ce7d181226848b36dc643cf31e498`, `internal/application/intelligence_context.go:13`. Output is schema/length/citation validated before persistence.
- **Cache boundary:** the key hashes repository, verified model, policy, prompt version/schema/task and exact bounded input. Only validated output is persisted; hits remain repository-scoped and each caller still publishes a separately audited snapshot-pinned result. Evidence: `inferenceCacheKey`, `baa3f04e534f65f2027c10e8f0a6910a6c6407ceb783ffcf87f1789f08fa49e7`, `internal/application/intelligence_cache.go:94-100`; `modelResponse.save`, `fccdcd3b6f18511b96efff566ed89ab240b497babd4d9976590ed4cc0ba1484e`, `internal/application/intelligence_cache.go:82-88`; `CacheAnalysis`, `ddc3deddf1be31e69fe2bf2c3140e491e1ff062164f8cb86186c7b6c1bb6e2b0`, `internal/store/postgres/analysis_cache.go:23-34`.
- **Model network policy:** local mode rejects API keys, URL credentials/query data, non-HTTP(S), `ollama.com`, and public literal IPs; cloud requires a bearer key and `https://ollama.com`, normalized to fixed cloud origin. Evidence: `validateMode`, `e4617e6d3875d30ce9179c9428f6533191221d49a1a81f5922b1a993c69297aa`, `internal/model/ollama/cloud.go:20-35`; `validateEndpoint`, `7193c636a093b4e32cb2e002fd55a62013c5d93ecfe7c49fea54d6615337ac11`, `internal/model/ollama/policy.go:62-77`; `validateHost`, `2855b98e48dec226eedc49f48dfefb75bd385dbbead8f8f6ce69f0e14dc5e64b`, `internal/model/ollama/policy.go:79-87`; `validateCloudEndpoint`, `c08575a1760c447421db86930e35edf24fcbe00b6823da2b86d6ffb762071886`, `internal/model/ollama/cloud.go:37-46`.

## Inferences

1. The design keeps structural retrieval useful without a model and exposes model-backed capabilities only when separately configured. This is an architectural inference from availability checks and capability output.
2. In cloud mode, bounded code evidence and the user question necessarily leave the local process for `ollama.com`; provider retention/training/region/deletion policy is not proven.
3. Separate answer/analysis clients likely permit distinct models and failures, while sharing store and repository scope.
4. The durable output cache can avoid repeat inference after publication failure, but exact crash behavior was not exercised.

## Unknowns

- Saved source was not executed; static call graphs are not proof of runtime order.
- Feature coverage is only 129/2,612 symbols; excluded/unindexed code, dynamic dispatch and newer source may change the picture.
- Latest and saved snapshots differ. Newer current routes, including `/answers/lookup`, are not attributed backward.
- Current model capabilities are disabled, so no live inference path was tested.
- Historical model identity `gpt-oss:20b@cloud:05afbac4bad6` is provider-reported, not a reproducible weights digest.
- TLS/reverse-proxy/network policy, database encryption/backups/retention, audit-outbox consumption, provider data policy and operator secret storage are unknown.
- No answer-read route exists in saved source; any intended later retrieval interface is unknown.

## Request accounting

- Attempts: 78
- Results: 77 HTTP 200; 1 sandbox-denied localhost `URLError` (`Operation not permitted`)
- Raw response bytes: 1,608,632
- Summed HTTP elapsed time: 853.735 ms
- OpenAPI: requested (3 attempts: 1 sandbox failure, 2 HTTP 200)
- Inference/Ollama: not requested
