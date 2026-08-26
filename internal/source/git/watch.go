package git

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

// Watch emits an initial snapshot, then reconciles the checkout at each interval.
// Slow callbacks serialize updates; no event queue can overflow or reorder snapshots.
// Cancellation returns ctx.Err(). Other capture/callback errors terminate the watch.
func (r *Repository) Watch(ctx context.Context, interval time.Duration, callback func(context.Context, Update) error) (err error) {
	ctx, span := tracer.Start(ctx, "source.watch")
	defer func() { finishSpan(span, err) }()
	if interval <= 0 {
		return errors.New("watch interval must be positive")
	}
	if callback == nil {
		return errors.New("watch callback is required")
	}
	previous, err := r.watchIteration(ctx, nil, callback)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			previous, err = r.watchIteration(ctx, previous, callback)
			if errors.Is(err, ErrUnstable) {
				continue
			}
			if err != nil {
				return err
			}
		}
	}
}

func (r *Repository) watchIteration(ctx context.Context, previous *Snapshot, callback func(context.Context, Update) error) (current *Snapshot, err error) {
	ctx, span := tracer.Start(ctx, "source.watch.iteration")
	defer func() { finishSpan(span, err) }()
	current, err = r.Capture(ctx)
	if err != nil {
		return previous, err
	}
	if previous != nil && previous.Fingerprint == current.Fingerprint {
		return previous, nil
	}
	changes := Diff(previous, current)
	span.SetAttributes(attribute.Int("source.changes", len(changes)))
	span.AddEvent("change")
	if err := callback(ctx, Update{Previous: previous, Current: current, Changes: changes}); err != nil {
		return previous, err
	}
	return current, nil
}
