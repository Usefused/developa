package httptransport

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"developa/internal/domain"
	"github.com/go-chi/chi/v5"
)

func (e *Explorer) flow(w http.ResponseWriter, r *http.Request) {
	options, err := flowOptionsFromQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	reader, ok := e.Knowledge.(domain.FlowReader)
	if !ok {
		writeError(w, domain.ErrNotConfigured)
		return
	}
	flow, err := reader.Flow(r.Context(), e.RepositoryID, chi.URLParam(r, "snapshot"), options)
	respond(w, flow, err)
}

func flowOptionsFromQuery(raw string) (domain.FlowOptions, error) {
	query, err := url.ParseQuery(raw)
	if err != nil {
		return domain.FlowOptions{}, domain.ErrInvalidInput
	}
	return parseFlowOptions(query)
}

func parseFlowOptions(query url.Values) (domain.FlowOptions, error) {
	options := domain.FlowOptions{SymbolID: query.Get("symbol_id"), FeatureID: query.Get("feature_id")}
	if !validFlowQuery(query) {
		return options, domain.ErrInvalidInput
	}
	var depthErr, limitErr error
	options.Depth, depthErr = flowInteger(query.Get("depth"))
	options.Limit, limitErr = flowInteger(query.Get("limit"))
	if err := errors.Join(depthErr, limitErr); err != nil {
		return options, err
	}
	return domain.NormalizeFlowOptions(options)
}

func validFlowQuery(query url.Values) bool {
	allowed := map[string]bool{"symbol_id": true, "feature_id": true, "depth": true, "limit": true}
	for key, values := range query {
		if len(values) != 1 || !allowed[key] || values[0] == "" {
			return false
		}
	}
	return true
}

func flowInteger(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < 1 {
		return 0, domain.ErrInvalidInput
	}
	return number, nil
}
