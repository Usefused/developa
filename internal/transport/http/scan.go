package httptransport

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

func (e *Explorer) scan(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeStatus(w, http.StatusForbidden, "cross_origin_forbidden")
		return
	}
	if !scanBody(w, r) {
		writeStatus(w, http.StatusBadRequest, "invalid_request")
		return
	}
	execution, err := e.Tracker.RequestScan(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, execution)
}

func scanBody(w http.ResponseWriter, r *http.Request) bool {
	if !jsonContentType(r.Header.Get("Content-Type")) {
		return false
	}
	// The repository is configured by the operator. Unknown fields are rejected
	// so callers cannot smuggle roots, actor identities, or model settings in.
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	var request struct{}
	err := decoder.Decode(&request)
	if errors.Is(err, io.EOF) {
		return true
	}
	if err != nil {
		return false
	}
	return errors.Is(decoder.Decode(&request), io.EOF)
}

func jsonContentType(header string) bool {
	if header == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(header)
	return err == nil && mediaType == "application/json"
}
