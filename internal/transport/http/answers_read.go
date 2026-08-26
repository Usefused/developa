package httptransport

import (
	"net/http"

	"developa/internal/domain"
	"github.com/go-chi/chi/v5"
)

func (e *Explorer) savedAnswer(w http.ResponseWriter, r *http.Request) {
	if !validMutation(w, r) {
		return
	}
	request, err := parseAnswer(w, r)
	if err != nil {
		writeError(w, err)
		return
	}
	reader, ok := e.Intelligence.(domain.SavedAnswerReader)
	if !ok {
		writeError(w, domain.ErrNotConfigured)
		return
	}
	// This read-only POST keeps questions out of URL/access logs and must not
	// require a live model, acquire an inference gate, or publish an answer.
	answer, err := reader.SavedAnswer(r.Context(), chi.URLParam(r, "snapshot"), request)
	respond(w, SavedAnswerResponse{Answer: answer}, err)
}
