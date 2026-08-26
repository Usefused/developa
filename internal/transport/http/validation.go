package httptransport

import (
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"developa/internal/domain"
	"github.com/go-chi/chi/v5"
)

var errInvalidFilter = errors.New("invalid filter")

func validateSnapshot(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validID(chi.URLParam(r, "snapshot")) {
			writeStatus(w, http.StatusBadRequest, "invalid_snapshot")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validID(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func parseFilter(query url.Values, defaultLimit int) (domain.Filter, error) {
	filter := domain.Filter{Query: query.Get("q"), Kind: query.Get("kind"), File: query.Get("file")}
	if !validQuery(filter.Query) || !validKind(filter.Kind) {
		return filter, errInvalidFilter
	}
	if filter.File != "" && !validPath(filter.File) {
		return filter, errInvalidFilter
	}
	var limitErr, offsetErr error
	filter.Limit, limitErr = boundedInteger(query.Get("limit"), defaultLimit, 1, 100)
	filter.Offset, offsetErr = boundedInteger(query.Get("offset"), 0, 0, 100000)
	return filter, errors.Join(limitErr, offsetErr)
}

func boundedInteger(value string, fallback, minimum, maximum int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	integer, err := strconv.Atoi(value)
	if err != nil || integer < minimum || integer > maximum {
		return 0, errInvalidFilter
	}
	return integer, nil
}

func validQuery(value string) bool {
	return len(value) <= 200 && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func validKind(value string) bool {
	kinds := map[string]bool{
		"": true, "function": true, "method": true, "struct": true,
		"interface": true, "interface_method": true, "alias": true,
		"named_type": true, "field": true, "constant": true,
		"variable": true, "closure": true,
	}
	return kinds[value]
}

func validPath(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return false
	}
	if path.IsAbs(value) || value == "." || path.Clean(value) != value {
		return false
	}
	return value != ".." && !strings.HasPrefix(value, "../")
}
