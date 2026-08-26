package httptransport

import (
	"log/slog"
	"net/http"

	"developa/internal/telemetry"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func traceRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := otel.Tracer("denverr/http").Start(ctx, "HTTP request", trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()
		span.AddEvent("execution.started")
		if span.SpanContext().IsValid() {
			w.Header().Set("X-Trace-ID", span.SpanContext().TraceID().String())
		}
		recorder := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(recorder, r.WithContext(ctx))
		finishRequest(span, recorder.Status(), chi.RouteContext(r.Context()))
	})
}

func finishRequest(span trace.Span, status int, route *chi.Context) {
	pattern := "unmatched"
	if route != nil && route.RoutePattern() != "" {
		pattern = route.RoutePattern()
	}
	// Only router templates are exported; an unmatched URL may contain secrets.
	span.SetName("HTTP " + pattern)
	span.SetAttributes(attribute.String("http.route", pattern), attribute.Int("http.response.status_code", status))
	if status >= http.StatusInternalServerError {
		telemetry.Fail(span, "http_server_error")
		return
	}
	span.AddEvent("execution.completed")
}

func recoverRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				// Panic payloads can include source or credentials, so do not print them.
				slog.ErrorContext(r.Context(), "HTTP request panicked")
				writeStatus(w, http.StatusInternalServerError, "internal_error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
