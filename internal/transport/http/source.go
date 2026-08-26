package httptransport

import (
	"math"
	"net/http"
	"net/url"

	"developa/internal/domain"
	"github.com/go-chi/chi/v5"
)

func (e *Explorer) symbolSource(w http.ResponseWriter, r *http.Request) {
	symbol := chi.URLParam(r, "symbol")
	if !validID(symbol) {
		writeStatus(w, http.StatusBadRequest, "invalid_symbol")
		return
	}
	options, err := parseSourceOptions(r.URL.RawQuery)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "invalid_source_options")
		return
	}
	reader, ok := e.Catalog.(domain.SymbolSourceReader)
	if !ok {
		writeError(w, domain.ErrSourceUnavailable)
		return
	}
	chunk, err := reader.Source(r.Context(), e.RepositoryID, chi.URLParam(r, "snapshot"), symbol, options)
	respond(w, chunk, err)
}

func parseSourceOptions(rawQuery string) (domain.SourceOptions, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return domain.SourceOptions{}, domain.ErrInvalidInput
	}
	for key, values := range query {
		if (key != "offset" && key != "limit") || len(values) != 1 || values[0] == "" {
			return domain.SourceOptions{}, domain.ErrInvalidInput
		}
	}
	limit, err := boundedInteger(query.Get("limit"), domain.DefaultSourceLimit, domain.MinSourceLimit, domain.MaxSourceLimit)
	if err != nil {
		return domain.SourceOptions{}, err
	}
	offset, err := boundedInteger(query.Get("offset"), 0, 0, math.MaxInt)
	return domain.SourceOptions{Limit: limit, Offset: offset}, err
}
