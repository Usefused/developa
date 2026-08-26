package ollama

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// ResolveModel verifies provider metadata without sending code or invoking inference.
// A cache may only reuse output after the same backend revision has been verified.
func (c *Client) ResolveModel(ctx context.Context) (identity string, err error) {
	ctx, cancel := context.WithTimeout(ctx, min(c.cfg.Timeout, 10*time.Second))
	defer cancel()
	ctx, span := otel.Tracer("developa/model/ollama").Start(ctx, "ollama.resolve_model")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, "model metadata unavailable")
		} else {
			span.SetAttributes(attribute.String("model.identity", identity))
			span.AddEvent("model.resolved")
		}
		span.End()
	}()
	span.SetAttributes(attribute.String("model.provider", "ollama"), attribute.String("model.name", c.cfg.Model),
		attribute.Bool("model.cloud", c.cfg.Cloud), attribute.Bool("model.source_transfer", false))
	select {
	case c.slots <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-c.slots }()
	if err := c.requireModel(ctx); err != nil {
		return "", err
	}
	return c.Model(), nil
}
