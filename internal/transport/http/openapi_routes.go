package httptransport

import "developa/internal/domain"

func openAPIEndpoints() []apiEndpoint {
	return []apiEndpoint{
		{Method: "GET", Path: "/healthz", ID: "health", Summary: "Process liveness", Public: true, Response: StatusResponse{}},
		{Method: "GET", Path: "/readyz", ID: "readiness", Summary: "PostgreSQL readiness", Public: true, Response: StatusResponse{}, Errors: []int{503}},
		{Method: "GET", Path: "/api/info", ID: "info", Summary: "Public setup and authentication flags", Public: true, Response: InfoResponse{}},
		{Method: "GET", Path: "/api/openapi.json", ID: "openAPI", Summary: "Download this OpenAPI document", Public: true},
		{Method: "GET", Path: "/api/repositories", ID: "repositories", Summary: "Search registered workspaces", Response: domain.RepositoryPage{}, Parameters: pageParameters(24), Description: "One SQL page; does not expose checkout roots. default_repository_id is used by compatibility routes."},
		{Method: "POST", Path: "/api/repositories", ID: "addWorkspace", Summary: "Persist and monitor a Git checkout", Response: domain.AddedWorkspace{}, Request: "workspace", Status: 201, Errors: []int{409, 422}, Description: "Select an existing Git working-tree root within WORKSPACE_ROOTS. Registration and audit commit before monitoring starts. Does not initialize Git, clone, or wait for indexing. Up to 32 workspaces; duplicate roots are idempotent. A non-Git/non-root folder returns 422 not_git_repository; an unavailable/outside folder returns 403 folder_forbidden; capacity returns 409 workspace_limit."},
		{Method: "POST", Path: "/api/repositories/resolve", ID: "resolveRepository", Summary: "Resolve a registered repository from its root path", Response: domain.RepositorySummary{}, Request: "repositoryPath", Errors: []int{404}, Description: "Read-only exact lookup by an absolute path visible to the engine. Symlinks are canonicalized when available. Returns repository identity and latest snapshot without echoing the path or invoking Git, indexing, or Ollama."},
		{Method: "GET", Path: "/api/workspace-roots", ID: "workspaceRoots", Summary: "List allowed engine filesystem locations", Response: []domain.FolderRoot{}, Description: "Authenticated operator-only server paths. This does not browse a remote caller's filesystem."},
		{Method: "GET", Path: "/api/workspace-folders", ID: "workspaceFolders", Summary: "Browse folders within an allowed location", Response: domain.FolderPage{}, Parameters: folderParameters(), Errors: []int{403}, Description: "At most 100 raw directory entries per page in native order; files, .git and symlink entries are omitted. Follow next_offset even when items is empty. Directory changes may shift pages. No source upload or inference."},
	}
}

func repositoryAPIEndpoints() []apiEndpoint {
	endpoints := []apiEndpoint{
		{Method: "GET", Path: "/project", ID: "project", Summary: "Read tracker status and latest snapshot", Response: domain.Project{}},
		{Method: "GET", Path: "/capabilities", ID: "capabilities", Summary: "Read configured retrieval and model capabilities", Response: CapabilitiesResponse{}, Description: "Configuration flags, not a live Ollama readiness test. Never downloads or invokes a model."},
		{Method: "POST", Path: "/scan", ID: "scan", Summary: "Queue a structural scan", Request: "empty", Status: 202, Response: domain.Execution{}, Errors: []int{409}, Description: "Admission to the process-local scan queue, not completion or restart-safe replay. Actor is derived from the shared operator token. Poll project for publication; 409 means busy. No AI is invoked by this request."},
	}
	for _, endpoint := range snapshotAPIEndpoints() {
		endpoint.Path = "/snapshots/{snapshot}" + endpoint.Path
		endpoint.Parameters = append([]apiSchema{pathParameter("snapshot")}, endpoint.Parameters...)
		endpoints = append(endpoints, endpoint)
	}
	return endpoints
}

func snapshotAPIEndpoints() []apiEndpoint {
	endpoints := []apiEndpoint{
		{Method: "GET", Path: "/files", ID: "files", Summary: "Search file building blocks", Parameters: catalogParameters(24), Response: domain.FilePage{}},
		{Method: "GET", Path: "/file", ID: "file", Summary: "Read a file and its imports", Parameters: requiredFileParameter(), Response: domain.FileDetail{}},
		{Method: "GET", Path: "/symbols", ID: "symbols", Summary: "Search functions, structs and other declarations", Parameters: catalogParameters(50), Response: domain.SymbolPage{}},
		{Method: "GET", Path: "/symbols/{symbol}", ID: "symbol", Summary: "Read a declaration and saved review", Parameters: []apiSchema{pathParameter("symbol")}, Response: domain.SymbolDetail{}},
		{Method: "GET", Path: "/symbols/{symbol}/source", ID: "symbolSource", Summary: "Read a bounded chunk of retained declaration source", Parameters: sourceParameters(), Response: domain.SymbolSource{}, Errors: []int{409}, Description: "Source is pinned to the requested repository and snapshot. offset is a zero-based UTF-8 byte offset relative to the declaration, not the file; a mid-rune or beyond-end offset returns 400. Chunks end at rune boundaries. Follow next_offset until null; complete describes retained declaration coverage, not whether this chunk is last. span, source_id and content_hash identify the retained declaration. Truncated historical previews without full retained source return 409 source_unavailable; inspect limitations on successful legacy reads. Unknown, repeated, empty and malformed query parameters are rejected."},
		{Method: "GET", Path: "/symbols/{symbol}/implementations", ID: "implementations", Summary: "Page through static interface implementation candidates", Parameters: implementationParameters(), Response: domain.ImplementationPage{}, Description: "Accepts a named interface symbol ID for all method links or an interface_method symbol ID for one method. An existing noninterface symbol may return an empty page; missing or foreign-scope symbols return 404. Candidates contain navigable interface, method, receiver and target source references, pointer receiver requirements and evidence. go_types_method_set is static type evidence, never proof of a runtime receiver. signature_match_with_unavailable_types is an unproven candidate from matching signatures when imported type evidence is unavailable; analysis remains partial. Inspect analysis and follow candidate target source before asserting behavior. Unknown, repeated, empty and malformed query parameters are rejected. No inference or dependency download."},
		{Method: "GET", Path: "/details", ID: "snapshotDetails", Summary: "Read snapshot diagnostics, changes and limitations", Response: domain.SnapshotDetails{}},
		{Method: "GET", Path: "/calls", ID: "calls", Summary: "Page through resolved and unresolved call sites", Parameters: callParameters(), Response: domain.CallPage{}, Description: "Local target, interface and interface_method references include symbol IDs and source positions when known. Interface dispatch remains unresolved: use its interface or interface_method symbol ID with the implementations endpoint to explore candidates, then inspect wiring and source. Candidate implementations are not resolved call edges."},
		{Method: "GET", Path: "/symbols/{symbol}/chain", ID: "callChain", Summary: "Traverse a bounded incoming or outgoing call chain", Parameters: chainParameters(), Response: domain.CallChain{}},
		{Method: "GET", Path: "/flow", ID: "codeFlow", Summary: "Get an application, function or feature flow", Parameters: flowParameters(), Response: domain.CodeFlow{}, Description: "symbol_id and feature_id are mutually exclusive. Without either, discover application roots. Returns nodes, edges, shared dependencies and cycles for UI rendering or agent traversal. Static calls are not runtime order. Unknown, repeated and empty query parameters are rejected."},
		{Method: "GET", Path: "/context", ID: "context", Summary: "Retrieve a bounded lexical source context pack", Parameters: []apiSchema{textParameter("q", 2000), integerParameter("limit", 12, 1, 20)}, Response: domain.ContextPack{}, Description: "Deterministic SQL retrieval; never invokes Ollama. An empty query is allowed."},
		{Method: "GET", Path: "/features", ID: "features", Summary: "Read cached inferred features", Parameters: pageParameters(24), Response: domain.FeaturePage{}, Description: "No inference. saved_snapshot is a navigation hint to older cached results, not evidence for the requested snapshot. Follow that snapshot explicitly."},
		{Method: "GET", Path: "/features/{feature}", ID: "feature", Summary: "Read an inferred feature and its source evidence", Parameters: []apiSchema{pathParameter("feature")}, Response: domain.Feature{}},
		{Method: "GET", Path: "/features/{feature}/context", ID: "featureContext", Summary: "Read agent-ready context for one feature", Parameters: featureContextParameters(), Response: domain.FeatureContextBundle{}, Description: "One bounded composition of the inferred feature claim, canonical source declarations, and its resolved static call flow. Uses three set-based SQL reads regardless of evidence or graph size and never invokes Ollama. Inspect limitations and the nested flow limitations before making completeness or runtime-order claims."},
	}
	return append(endpoints, intelligenceAPIEndpoints()...)
}

func requiredFileParameter() []apiSchema {
	parameter := textParameter("path", 4096)
	parameter["required"] = true
	parameter["description"] = "Canonical repository-relative file path, not an absolute path; no . or .. traversal."
	return []apiSchema{parameter}
}

func intelligenceAPIEndpoints() []apiEndpoint {
	return []apiEndpoint{
		{Method: "POST", Path: "/features/generate", ID: "generateFeatures", Summary: "Queue or resume feature discovery", Request: "empty", Status: 202, Response: domain.AnalysisJob{}, Errors: []int{409}, Description: "Durable PostgreSQL job admission. Parsing and inference run independently of HTTP lifetime. Unchanged source can reuse inference caches. Poll analysis-job or subscribe to events; an accepted job is not completed analysis."},
		{Method: "GET", Path: "/analysis-job", ID: "analysisJob", Summary: "Read saved feature analysis progress", Response: domain.AnalysisJob{}, Description: "A snapshot with no job returns status not_queued. Existing durable status remains readable when model execution is disabled."},
		{Method: "GET", Path: "/events", ID: "analysisEvents", Summary: "Subscribe to saved analysis progress", Stream: true, Events: map[string]any{"analysis": domain.AnalysisJob{}}, Description: "Read-only SSE sends current status immediately and then persisted changes. Lifetime is five minutes; reconnect to read current state. Disconnect does not cancel an accepted background job."},
		{Method: "POST", Path: "/answers", ID: "answer", Summary: "Answer a code question with validated source evidence", Request: "answer", Response: domain.Answer{}, Errors: []int{409, 502, 504}, Description: "Synchronous explicit inference or matching saved answer. Questions remain in the request body. A nonempty symbol_id, feature_id, or flow focuses the bounded context; at most one target is allowed. Disconnect cancels the request."},
		{Method: "POST", Path: "/answers/lookup", ID: "savedAnswer", Summary: "Look up a saved explanation without inference", Request: "answer", Response: SavedAnswerResponse{}, Description: "Read-only POST; no model availability check, inference, or new publication. Returns answer:null when unchanged source/request context has no saved match. Question stays out of URLs."},
		{Method: "POST", Path: "/answers/stream", ID: "answerStream", Summary: "Stream answer progress and the saved result", Request: "answer", Stream: true, Events: map[string]any{"started": StreamStarted{}, "answer": domain.Answer{}}, Errors: []int{409, 502, 504}, Description: "started, then answer or error with heartbeat comments while waiting. The final answer is validated and persisted before emission; no raw provider tokens are exposed. Do not automatically retry a disconnected POST."},
		{Method: "GET", Path: "/function-reviews", ID: "functionReviews", Summary: "Read a bounded page of saved function reviews", Parameters: reviewParameters(), Response: domain.ReviewPage{}, Description: "symbol_id and callee_of are mutually exclusive. callee_of selects distinct resolved callees; no target pages reviewable declarations. Reports unresolved_count and next_offset. Never invokes inference."},
		{Method: "POST", Path: "/function-reviews", ID: "reviewFunctions", Summary: "Review a bounded function batch", Request: "review", Response: domain.ReviewPage{}, Errors: []int{409, 502, 504}, Description: "Explicit synchronous batch of at most eight records (default four), with per-function cache reuse. Use next_offset for the next separate request. Parameter descriptions refer to zero-based parser positions."},
		{Method: "POST", Path: "/function-reviews/stream", ID: "reviewFunctionsStream", Summary: "Stream review progress and the persisted batch", Request: "review", Stream: true, Events: map[string]any{"started": StreamStarted{}, "reviews": domain.ReviewPage{}}, Errors: []int{409, 502, 504}, Description: "Same bounded review request; started then reviews or error. Final review content is saved before emission. Cancellation does not automatically replay inference."},
	}
}
