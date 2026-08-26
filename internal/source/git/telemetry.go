package git

import (
	"errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("developa/internal/source/git")

func finishSpan(span trace.Span, err error) {
	defer span.End()
	if err != nil {
		// Error strings from filesystem operations can disclose excluded file names.
		span.RecordError(errors.New("source operation failed"))
		span.SetStatus(codes.Error, "source operation failed")
		span.AddEvent("failure")
		return
	}
	span.AddEvent("completion")
}
