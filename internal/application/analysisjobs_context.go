package application

import (
	"context"

	"developa/internal/domain"
)

type analysisLeaseKey struct{}
type analysisLease struct{ jobID, token string }

func withAnalysisLease(ctx context.Context, job domain.AnalysisJob) context.Context {
	return context.WithValue(ctx, analysisLeaseKey{}, analysisLease{jobID: job.ID, token: job.LeaseToken})
}

func applyAnalysisLease(ctx context.Context, execution domain.Execution) domain.Execution {
	lease, ok := ctx.Value(analysisLeaseKey{}).(analysisLease)
	if !ok {
		return execution
	}
	// The durable queue identifies the originating request; background attempts have a system actor.
	execution.Actor = "system"
	execution.JobID, execution.LeaseToken = lease.jobID, lease.token
	return execution
}
