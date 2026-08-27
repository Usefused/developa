package httptransport

import (
	"errors"
	"net/http"
	"net/url"

	"developa/internal/domain"
	"github.com/go-chi/chi/v5"
)

func (e *Explorer) mountKnowledge(router chi.Router) {
	router.Post("/features/generate", e.generateFeatures)
	router.Get("/analysis-job", e.analysisJob)
	router.Get("/events", e.analysisEvents)
	router.Group(func(api chi.Router) {
		api.Use(e.requireKnowledge)
		api.Get("/calls", e.calls)
		api.Get("/flow", e.flow)
		api.Get("/symbols/{symbol}/chain", e.chain)
		api.Get("/context", e.context)
		api.Get("/features", e.features)
		api.Get("/features/{feature}", e.feature)
		api.Get("/features/{feature}/context", e.featureContext)
		api.Post("/answers", e.answer)
		api.Post("/answers/stream", e.answerStream)
		api.Post("/answers/lookup", e.savedAnswer)
		api.Get("/function-reviews", e.functionReviews)
		api.Post("/function-reviews", e.reviewFunctions)
		api.Post("/function-reviews/stream", e.reviewFunctionsStream)
	})
}

func (e *Explorer) requireKnowledge(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e.Knowledge == nil {
			writeStatus(w, http.StatusServiceUnavailable, "not_configured")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (e *Explorer) capabilities(w http.ResponseWriter, _ *http.Request) {
	analysisAvailable := e.Jobs != nil && e.Jobs.Available()
	answerAvailable := e.Intelligence != nil && e.Intelligence.Available()
	_, flowAvailable := e.Knowledge.(domain.FlowReader)
	_, reviewAvailable := e.Knowledge.(domain.ReviewStore)
	reviewGeneration := reviewAvailable && e.Reviewer != nil && e.Reviewer.Available()
	writeJSON(w, http.StatusOK, CapabilitiesResponse{
		Calls: e.Knowledge != nil, Flows: flowAvailable, Context: e.Knowledge != nil, Features: e.Knowledge != nil,
		AnalysisJobs: analysisAvailable,
		Answers:      answerAvailable, AutomaticFeatures: e.AutomaticFeatures && analysisAvailable,
		OllamaConfigured: analysisAvailable || answerAvailable || reviewGeneration, OllamaCloud: e.OllamaCloud,
		FunctionReviews: reviewAvailable, ReviewGeneration: reviewGeneration,
	})
}

func (e *Explorer) calls(w http.ResponseWriter, r *http.Request) {
	filter, err := parseCallFilter(r.URL.Query())
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "invalid_filter")
		return
	}
	page, err := e.Knowledge.Calls(r.Context(), e.RepositoryID, chi.URLParam(r, "snapshot"), filter)
	respond(w, page, err)
}

func (e *Explorer) chain(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "symbol")
	options, err := parseChainOptions(r.URL.Query())
	if err != nil || !validID(id) {
		writeStatus(w, http.StatusBadRequest, "invalid_filter")
		return
	}
	chain, err := e.Knowledge.Chain(r.Context(), e.RepositoryID, chi.URLParam(r, "snapshot"), id, options)
	respond(w, chain, err)
}

func (e *Explorer) context(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	limit, err := boundedInteger(r.URL.Query().Get("limit"), 12, 1, 20)
	if err != nil || !validQuestion(query, true) {
		writeStatus(w, http.StatusBadRequest, "invalid_filter")
		return
	}
	pack, err := e.Knowledge.Context(r.Context(), e.RepositoryID, chi.URLParam(r, "snapshot"), query, limit)
	respond(w, pack, err)
}

func (e *Explorer) features(w http.ResponseWriter, r *http.Request) {
	filter, err := parseFilter(r.URL.Query(), 24)
	if err != nil || filter.File != "" || filter.Kind != "" {
		writeStatus(w, http.StatusBadRequest, "invalid_filter")
		return
	}
	page, err := e.Knowledge.Features(r.Context(), e.RepositoryID, chi.URLParam(r, "snapshot"), filter)
	respond(w, page, err)
}

func (e *Explorer) feature(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "feature")
	if !validID(id) {
		writeStatus(w, http.StatusBadRequest, "invalid_feature")
		return
	}
	feature, err := e.Knowledge.Feature(r.Context(), e.RepositoryID, chi.URLParam(r, "snapshot"), id)
	respond(w, feature, err)
}

func (e *Explorer) featureContext(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "feature")
	options, err := parseFeatureContextOptions(r)
	if err != nil || !validID(id) {
		writeStatus(w, http.StatusBadRequest, "invalid_filter")
		return
	}
	if e.FeatureContexts == nil {
		writeError(w, domain.ErrNotConfigured)
		return
	}
	bundle, err := e.FeatureContexts.FeatureContext(r.Context(), chi.URLParam(r, "snapshot"), id, options)
	respond(w, bundle, err)
}

func parseFeatureContextOptions(r *http.Request) (domain.FeatureContextOptions, error) {
	query := r.URL.Query()
	if !onlyQueryKeys(query, "source_limit", "depth", "flow_limit") {
		return domain.FeatureContextOptions{}, domain.ErrInvalidInput
	}
	var options domain.FeatureContextOptions
	var sourceErr, depthErr, flowErr error
	options.SourceLimit, sourceErr = boundedInteger(query.Get("source_limit"), 20, 1, 20)
	options.Depth, depthErr = boundedInteger(query.Get("depth"), 6, 1, 12)
	options.FlowLimit, flowErr = boundedInteger(query.Get("flow_limit"), 80, 1, 100)
	return options, errors.Join(sourceErr, depthErr, flowErr)
}

func onlyQueryKeys(query url.Values, allowed ...string) bool {
	known := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		known[key] = true
	}
	for key, values := range query {
		if !known[key] || len(values) != 1 || values[0] == "" {
			return false
		}
	}
	return true
}

func (e *Explorer) generateFeatures(w http.ResponseWriter, r *http.Request) {
	if !validMutation(w, r) {
		return
	}
	if !scanBody(w, r) {
		writeStatus(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if e.Jobs == nil || !e.Jobs.Available() {
		writeError(w, domain.ErrModelUnavailable)
		return
	}
	job, err := e.Jobs.Queue(r.Context(), chi.URLParam(r, "snapshot"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (e *Explorer) analysisJob(w http.ResponseWriter, r *http.Request) {
	if e.Jobs == nil {
		writeError(w, domain.ErrNotConfigured)
		return
	}
	// Disabled workers must not hide the durable status of already queued work.
	job, err := e.Jobs.Status(r.Context(), chi.URLParam(r, "snapshot"))
	respond(w, job, err)
}

func (e *Explorer) answer(w http.ResponseWriter, r *http.Request) {
	request, ok := e.prepareAnswer(w, r)
	if !ok {
		return
	}
	answer, err := e.Intelligence.Answer(r.Context(), chi.URLParam(r, "snapshot"), request)
	respond(w, answer, err)
}

func (e *Explorer) prepareAnswer(w http.ResponseWriter, r *http.Request) (domain.AnswerRequest, bool) {
	if !validMutation(w, r) {
		return domain.AnswerRequest{}, false
	}
	request, err := parseAnswer(w, r)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "invalid_request")
		return request, false
	}
	if e.Intelligence == nil || !e.Intelligence.Available() {
		writeError(w, domain.ErrModelUnavailable)
		return request, false
	}
	return request, true
}

func validMutation(w http.ResponseWriter, r *http.Request) bool {
	if sameOrigin(r) {
		return true
	}
	writeStatus(w, http.StatusForbidden, "cross_origin_forbidden")
	return false
}
