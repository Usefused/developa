// Package telemetry configures source-free operational tracing. Durable audit
// records belong to application transactions once mutation services exist.
package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type Config struct {
	ServiceName string
	// Endpoint is the OTLP HTTP base URL; traces are sent to its /v1/traces path.
	Endpoint string
	Disabled bool
}

func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	if cfg.ServiceName == "" {
		return nil, errors.New("telemetry service name is required")
	}
	if cfg.Disabled {
		// A true no-op provider avoids generating span data while retaining W3C
		// context propagation for callers that forward an incoming trace.
		otel.SetTracerProvider(trace.NewNoopTracerProvider())
		otel.SetTextMapPropagator(propagation.TraceContext{})
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := newExporter(ctx, cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithExportTimeout(5*time.Second)),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", cfg.ServiceName))),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	)
	otel.SetTracerProvider(provider)
	// Baggage is deliberately excluded: callers may place source or credentials in it.
	otel.SetTextMapPropagator(propagation.TraceContext{})
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {
		slog.Error("OpenTelemetry export failed")
	}))
	return provider.Shutdown, nil
}

func ParseSDKDisabled(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("OTEL_SDK_DISABLED must be true or false")
	}
}

func newExporter(ctx context.Context, endpoint string) (sdktrace.SpanExporter, error) {
	if endpoint == "" {
		return stdouttrace.New(stdouttrace.WithWriter(os.Stderr))
	}
	if err := validateEndpoint(endpoint); err != nil {
		return nil, err
	}
	tracesURL, err := url.JoinPath(endpoint, "v1/traces")
	if err != nil {
		return nil, errors.New("telemetry endpoint must be a valid HTTP(S) URL")
	}
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(tracesURL),
		otlptracehttp.WithTimeout(5*time.Second),
	)
	if err != nil {
		return nil, errors.New("telemetry exporter initialization failed")
	}
	return exporter, nil
}

func validateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return errors.New("telemetry endpoint must be a valid HTTP(S) URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("telemetry endpoint must use HTTP(S)")
	}
	if u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("telemetry endpoint requires a host and no credentials, query, or fragment")
	}
	return nil
}

// Fail accepts a stable, content-free description, never an underlying driver error.
func Fail(span trace.Span, reason string) {
	span.SetStatus(codes.Error, reason)
	span.AddEvent("execution.failed", trace.WithAttributes(attribute.String("error.type", reason)))
}
