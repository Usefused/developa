package httptransport

import (
	"errors"
	"net/http"
	"net/url"

	"developa/internal/domain"
	"github.com/go-chi/chi/v5"
)

func (e *Explorer) implementations(w http.ResponseWriter, r *http.Request) {
	symbol := chi.URLParam(r, "symbol")
	if !validID(symbol) {
		writeStatus(w, http.StatusBadRequest, "invalid_symbol")
		return
	}
	options, err := implementationOptionsFromQuery(r.URL.RawQuery)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "invalid_filter")
		return
	}
	reader, ok := e.Knowledge.(domain.ImplementationReader)
	if !ok {
		writeError(w, domain.ErrNotConfigured)
		return
	}
	page, err := reader.Implementations(r.Context(), e.RepositoryID, chi.URLParam(r, "snapshot"), symbol, options)
	respond(w, page, err)
}

func implementationOptionsFromQuery(raw string) (domain.ImplementationOptions, error) {
	query, err := url.ParseQuery(raw)
	if err != nil {
		return domain.ImplementationOptions{}, errInvalidFilter
	}
	for key, values := range query {
		if len(values) != 1 || values[0] == "" || (key != "limit" && key != "offset") {
			return domain.ImplementationOptions{}, errInvalidFilter
		}
	}
	var options domain.ImplementationOptions
	var limitErr, offsetErr error
	options.Limit, limitErr = boundedInteger(query.Get("limit"), 20, 1, 100)
	options.Offset, offsetErr = boundedInteger(query.Get("offset"), 0, 0, 100000)
	return options, errors.Join(limitErr, offsetErr)
}
