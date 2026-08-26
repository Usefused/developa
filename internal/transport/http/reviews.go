package httptransport

import (
	"context"
	"net/http"

	"developa/internal/domain"
	"github.com/go-chi/chi/v5"
)

func (e *Explorer) functionReviews(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	options := domain.ReviewOptions{SymbolID: query.Get("symbol_id"), CalleeOf: query.Get("callee_of")}
	var err error
	options.Limit, err = boundedInteger(query.Get("limit"), 4, 1, 8)
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	options.Offset, err = boundedInteger(query.Get("offset"), 0, 0, 100000)
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	page, err := e.readReviews(r.Context(), chi.URLParam(r, "snapshot"), options)
	respond(w, page, err)
}

func (e *Explorer) readReviews(ctx context.Context, snapshot string, options domain.ReviewOptions) (domain.ReviewPage, error) {
	store, ok := e.Knowledge.(domain.ReviewStore)
	if !ok {
		return domain.ReviewPage{}, domain.ErrNotConfigured
	}
	return store.ReviewPage(ctx, e.RepositoryID, snapshot, options)
}

func (e *Explorer) reviewFunctions(w http.ResponseWriter, r *http.Request) {
	options, ok := e.prepareReviews(w, r)
	if !ok {
		return
	}
	page, err := e.Reviewer.Review(r.Context(), chi.URLParam(r, "snapshot"), options)
	respond(w, page, err)
}

func (e *Explorer) prepareReviews(w http.ResponseWriter, r *http.Request) (domain.ReviewOptions, bool) {
	var options domain.ReviewOptions
	if !validMutation(w, r) {
		return options, false
	}
	if err := decodeRequest(w, r, &options, 2048); err != nil {
		writeError(w, err)
		return options, false
	}
	options, err := domain.NormalizeReviewOptions(options)
	if err != nil {
		writeError(w, err)
		return options, false
	}
	if e.Reviewer == nil || !e.Reviewer.Available() {
		writeError(w, domain.ErrModelUnavailable)
		return options, false
	}
	return options, true
}

func (e *Explorer) reviewFunctionsStream(w http.ResponseWriter, r *http.Request) {
	options, ok := e.prepareReviews(w, r)
	if !ok {
		return
	}
	snapshot := chi.URLParam(r, "snapshot")
	if _, err := e.readReviews(r.Context(), snapshot, options); err != nil {
		writeError(w, err)
		return
	}
	writer := newEventWriter(w)
	if writer.event("started", StreamStarted{Status: "started", TraceID: eventTraceID(r.Context())}) != nil {
		return
	}
	streamSavedResult(writer, r, "reviews", func(ctx context.Context) (any, error) { return e.Reviewer.Review(ctx, snapshot, options) })
}
