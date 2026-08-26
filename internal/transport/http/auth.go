package httptransport

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func (e *Explorer) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (!e.configured() && !e.managementEnabled()) || len(e.Token) < 24 {
			writeStatus(w, http.StatusServiceUnavailable, "not_configured")
			return
		}
		if !authorized(r, e.Token) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeStatus(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		trace.SpanFromContext(r.Context()).SetAttributes(attribute.String("actor.type", "operator"))
		next.ServeHTTP(w, r)
	})
}

func authorized(r *http.Request, token string) bool {
	if len(r.Header.Values("Authorization")) != 1 {
		return false
	}
	scheme, credential, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	// Hashing makes comparison fixed-length even for an invalid token length.
	provided, expected := sha256.Sum256([]byte(credential)), sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(provided[:], expected[:]) == 1
}

func sameOrigin(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	origins := r.Header.Values("Origin")
	if len(origins) == 0 {
		return true
	}
	if len(origins) != 1 {
		return false
	}
	return matchesOrigin(r, origins[0])
}

func matchesOrigin(r *http.Request, raw string) bool {
	origin, err := url.Parse(raw)
	if err != nil || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return origin.Scheme == scheme && strings.EqualFold(origin.Host, r.Host) && origin.Path == ""
}
