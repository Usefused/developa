package httptransport

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"developa/internal/domain"
)

func parseCallFilter(query url.Values) (domain.CallFilter, error) {
	filter := domain.CallFilter{SymbolID: query.Get("symbol_id"), Direction: query.Get("direction"), Resolution: query.Get("resolution")}
	if filter.Direction == "" {
		filter.Direction = "out"
	}
	if filter.SymbolID != "" && !validID(filter.SymbolID) {
		return filter, errInvalidFilter
	}
	if !validDirection(filter.Direction) || !validResolution(filter.Resolution) {
		return filter, errInvalidFilter
	}
	var limitErr, offsetErr error
	filter.Limit, limitErr = boundedInteger(query.Get("limit"), 50, 1, 100)
	filter.Offset, offsetErr = boundedInteger(query.Get("offset"), 0, 0, 100000)
	return filter, errors.Join(limitErr, offsetErr)
}

func parseChainOptions(query url.Values) (domain.ChainOptions, error) {
	options := domain.ChainOptions{Direction: query.Get("direction")}
	if options.Direction == "" {
		options.Direction = "out"
	}
	if !validDirection(options.Direction) {
		return options, errInvalidFilter
	}
	var depthErr, limitErr error
	options.Depth, depthErr = boundedInteger(query.Get("depth"), 2, 1, 5)
	options.Limit, limitErr = boundedInteger(query.Get("limit"), 40, 1, 100)
	return options, errors.Join(depthErr, limitErr)
}

func validDirection(value string) bool { return value == "in" || value == "out" }

func validResolution(value string) bool {
	return map[string]bool{"": true, "resolved": true, "unresolved": true, "external": true, "builtin": true}[value]
}

func validQuestion(value string, allowEmpty bool) bool {
	if !allowEmpty && strings.TrimSpace(value) == "" {
		return false
	}
	return len(value) <= 2000 && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func parseAnswer(w http.ResponseWriter, r *http.Request) (domain.AnswerRequest, error) {
	var request domain.AnswerRequest
	if err := decodeRequest(w, r, &request, 16<<10); err != nil {
		return request, err
	}
	if !validQuestion(request.Question, false) || !validAnswerTarget(&request) {
		return request, domain.ErrInvalidInput
	}
	return request, nil
}

func decodeRequest(w http.ResponseWriter, r *http.Request, target any, limit int64) error {
	if !jsonContentType(r.Header.Get("Content-Type")) {
		return domain.ErrInvalidInput
	}
	// JSON escapes can use six bytes for one decoded ASCII byte; bound both representations.
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return domain.ErrInvalidInput
	}
	if !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
		return domain.ErrInvalidInput
	}
	return nil
}

func validAnswerTarget(request *domain.AnswerRequest) bool {
	if request.Flow != nil {
		return normalizeAnswerFlow(request)
	}
	if request.SymbolID != "" && request.FeatureID != "" {
		return false
	}
	return (request.SymbolID == "" || validID(request.SymbolID)) && (request.FeatureID == "" || validID(request.FeatureID))
}

func normalizeAnswerFlow(request *domain.AnswerRequest) bool {
	if request.SymbolID != "" || request.FeatureID != "" {
		return false
	}
	options, err := domain.NormalizeFlowOptions(*request.Flow)
	if err != nil {
		return false
	}
	request.Flow = &options
	return true
}
