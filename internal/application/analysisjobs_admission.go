package application

import (
	"context"
	"sync"
)

// AnalysisAdmission bounds background inference across repositories in one process.
// Waiting happens before claiming a durable lease, so queue time cannot consume its TTL.
type AnalysisAdmission struct {
	once sync.Once
	slot chan struct{}
}

func NewAnalysisAdmission() *AnalysisAdmission {
	return &AnalysisAdmission{}
}

func (a *AnalysisAdmission) acquire(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a == nil {
		return func() {}, nil
	}
	a.once.Do(func() { a.slot = make(chan struct{}, 1) })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case a.slot <- struct{}{}:
		return func() { <-a.slot }, nil
	}
}
