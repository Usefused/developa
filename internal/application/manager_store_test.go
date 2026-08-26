package application

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"developa/internal/domain"
)

type executionRecord struct {
	execution    domain.Execution
	outcome      string
	contextValue any
}

type testContextKey struct{}

type managerStore struct {
	mu           sync.Mutex
	repository   domain.Repository
	latest       *domain.Snapshot
	reports      []Report
	executions   []executionRecord
	failSave     bool
	failAudit    bool
	gate         <-chan struct{}
	auditGate    <-chan struct{}
	auditEntered chan struct{}
	entered      chan struct{}
	saving       int
	maxSaving    int
}

func newManagerStore() *managerStore { return &managerStore{entered: make(chan struct{}, 32)} }

func (s *managerStore) EnsureRepository(_ context.Context, repo domain.Repository) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repository = repo
	return nil
}

func (s *managerStore) Latest(context.Context, string) (domain.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest == nil {
		return domain.Snapshot{}, domain.ErrNotFound
	}
	return *s.latest, nil
}

func (s *managerStore) SaveSnapshot(ctx context.Context, _ string, report Report, execution domain.Execution) (domain.Snapshot, error) {
	s.mu.Lock()
	s.saving++
	if s.saving > s.maxSaving {
		s.maxSaving = s.saving
	}
	gate, fail := s.gate, s.failSave
	s.mu.Unlock()
	defer s.finishSave()
	s.entered <- struct{}{}
	if err := waitSaveGate(ctx, gate); err != nil {
		return domain.Snapshot{}, err
	}
	if fail {
		return domain.Snapshot{}, errors.New("PRIVATE-STORAGE-ERROR")
	}
	return s.saveReport(ctx, report, execution), nil
}

func waitSaveGate(ctx context.Context, gate <-chan struct{}) error {
	if gate == nil {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-gate:
		return nil
	}
}

func (s *managerStore) finishSave() { s.mu.Lock(); s.saving--; s.mu.Unlock() }

func (s *managerStore) saveReport(ctx context.Context, report Report, execution domain.Execution) domain.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := domain.Snapshot{IndexVersion: domain.IndexVersion, ID: fmt.Sprint(len(s.reports) + 1), Fingerprint: report.Snapshot.Fingerprint,
		Completeness: string(report.Index.Completeness), ChangesKnown: report.ChangesKnown, IndexedAt: time.Now(), Tags: report.Snapshot.Tags}
	s.reports = append(s.reports, report)
	s.executions = append(s.executions, executionRecord{execution: execution, outcome: "completed", contextValue: ctx.Value(testContextKey{})})
	s.latest = &snapshot
	return snapshot
}

func (s *managerStore) RecordExecution(ctx context.Context, _ string, execution domain.Execution, outcome string) error {
	if err := s.awaitQueuedAudit(ctx, outcome); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failAudit {
		return errors.New("PRIVATE-AUDIT-ERROR")
	}
	s.executions = append(s.executions, executionRecord{execution: execution, outcome: outcome, contextValue: ctx.Value(testContextKey{})})
	return nil
}

func (s *managerStore) awaitQueuedAudit(ctx context.Context, outcome string) error {
	if outcome != "queued" {
		return nil
	}
	s.mu.Lock()
	gate, entered := s.auditGate, s.auditEntered
	s.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	return waitSaveGate(ctx, gate)
}

func (s *managerStore) blockAudit(gate <-chan struct{}) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auditGate, s.auditEntered = gate, make(chan struct{}, 1)
	return s.auditEntered
}

func (*managerStore) Files(context.Context, string, string, domain.Filter) (domain.FilePage, error) {
	return domain.FilePage{}, domain.ErrNotFound
}
func (*managerStore) File(context.Context, string, string, string) (domain.FileDetail, error) {
	return domain.FileDetail{}, domain.ErrNotFound
}
func (*managerStore) Symbols(context.Context, string, string, domain.Filter) (domain.SymbolPage, error) {
	return domain.SymbolPage{}, domain.ErrNotFound
}
func (*managerStore) Symbol(context.Context, string, string, string) (domain.SymbolDetail, error) {
	return domain.SymbolDetail{}, domain.ErrNotFound
}
func (*managerStore) Details(context.Context, string, string) (domain.SnapshotDetails, error) {
	return domain.SnapshotDetails{}, domain.ErrNotFound
}

func (s *managerStore) report(t *testing.T, index int) Report {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reports) <= index {
		t.Fatalf("report %d unavailable", index)
	}
	return s.reports[index]
}

func (s *managerStore) setFailure(save, audit bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failSave, s.failAudit = save, audit
}

func (s *managerStore) setGate(gate <-chan struct{}) { s.mu.Lock(); s.gate = gate; s.mu.Unlock() }

func (s *managerStore) count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.reports) }

func (s *managerStore) hasOutcome(id, outcome string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.executions {
		if record.execution.ID == id && record.outcome == outcome {
			return true
		}
	}
	return false
}
