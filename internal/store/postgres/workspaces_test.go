package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"developa/internal/domain"
)

func workspaceRecords(t *testing.T, store *Store, start, count int) []domain.WorkspaceRegistration {
	t.Helper()
	records := []domain.WorkspaceRegistration{}
	for i := start; i < start+count; i++ {
		repo := domain.Repository{ID: fmt.Sprintf("workspace-%d", i), Name: fmt.Sprintf("Workspace %d", i)}
		if err := store.EnsureRepository(context.Background(), repo); err != nil {
			t.Fatal(err)
		}
		records = append(records, domain.WorkspaceRegistration{Repository: repo, Root: fmt.Sprintf("/repos/workspace-%d", i)})
	}
	return records
}

func workspaceExecution() domain.Execution {
	return domain.Execution{ID: "workspace-test", Actor: "operator", Trigger: "workspace.add"}
}

func TestIntegrationWorkspaceRegistrationUsesConstantQueriesAndDurableAudits(t *testing.T) {
	store, counter := catalogFixture(t)
	for _, size := range []int{1, 16} {
		records := workspaceRecords(t, store, 0, size)
		counter.Store(0)
		if err := store.SaveWorkspaces(context.Background(), records, workspaceExecution()); err != nil {
			t.Fatal(err)
		}
		if counter.Load() != 6 {
			t.Fatalf("registration query count scales: %d", counter.Load())
		}
		counter.Store(0)
		saved, err := store.SavedWorkspaces(context.Background(), nil)
		if err != nil || len(saved) != size || counter.Load() != 1 {
			t.Fatal("registry restore lost its single-query bound", err, counter.Load())
		}
	}
	assertWorkspaceRegistrationAudit(t, store)
}

func assertWorkspaceRegistrationAudit(t *testing.T, store *Store) {
	t.Helper()
	var count int
	err := store.pool.QueryRow(context.Background(), `SELECT count(*) FROM developa_audit_events e JOIN developa_audit_outbox o ON o.event_id=e.id WHERE trigger='workspace.add' AND actor='operator'`).Scan(&count)
	if err != nil || count != 17 {
		t.Fatal("workspace additions lost durable audit/outbox records", err, count)
	}
	saved, err := store.SavedWorkspaces(context.Background(), []string{"workspace-0"})
	if err != nil || len(saved) != 15 {
		t.Fatal("SQL seed exclusion did not apply", err)
	}
}

func TestIntegrationWorkspaceCapacityAndFailuresAreTransactional(t *testing.T) {
	store, _ := catalogFixture(t)
	records := workspaceRecords(t, store, 0, 32)
	if err := store.SaveWorkspaces(context.Background(), records, workspaceExecution()); err != nil {
		t.Fatal(err)
	}
	extra := workspaceRecords(t, store, 32, 1)
	if err := store.SaveWorkspaces(context.Background(), extra, workspaceExecution()); !errors.Is(err, domain.ErrWorkspaceLimit) {
		t.Fatal("workspace cap not enforced", err)
	}
	if err := store.SaveWorkspaces(context.Background(), records[:1], workspaceExecution()); err != nil {
		t.Fatal("duplicate at capacity rejected", err)
	}
	saved, err := store.SavedWorkspaces(context.Background(), nil)
	if err != nil || len(saved) != 32 {
		t.Fatal("failed registration was published", err)
	}
}
