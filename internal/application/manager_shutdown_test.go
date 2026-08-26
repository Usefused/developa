package application

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestManagerCloseWaitsForRequestAdmission(t *testing.T) {
	store := newManagerStore()
	manager := fixtureManager(t, fixtureRepository(t, "package fixture\n"), store, time.Hour)
	awaitReady(t, manager, "1")
	entered := store.blockAudit(make(chan struct{}))
	result := make(chan error, 1)
	go func() { _, err := manager.RequestScan(context.Background()); result <- err }()
	awaitSignal(t, entered)
	manager.Close()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("request accepted after admission was canceled")
		}
	case <-time.After(time.Second):
		t.Fatal("Close returned before request admission completed")
	}
	if len(manager.requests) != 0 {
		t.Fatal("shutdown left queued work")
	}
}

func TestManagerLifecycleCancellationClosesAdmission(t *testing.T) {
	store := newManagerStore()
	root := fixtureRepository(t, "package fixture\n")
	manager, err := NewManager(context.Background(), store, ManagerConfig{RepositoryPath: root, PollInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	ctx, cancel := context.WithCancel(context.Background())
	manager.Start(ctx)
	awaitReady(t, manager, "1")
	entered := store.blockAudit(make(chan struct{}))
	result := make(chan error, 1)
	go func() { _, err := manager.RequestScan(context.Background()); result <- err }()
	awaitSignal(t, entered)
	cancel()
	manager.Close()
	if err := <-result; err == nil {
		t.Fatal("canceled lifecycle accepted queued work")
	}
	if len(manager.requests) != 0 {
		t.Fatal("lifecycle cancellation orphaned work")
	}
}

func TestManagerShutdownCancelsAlreadyQueuedWork(t *testing.T) {
	store := newManagerStore()
	manager, err := NewManager(context.Background(), store, ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	request := newScanRequest(context.Background(), "operator", "manual")
	manager.requests <- request
	manager.shutdown()
	if !store.hasOutcome(request.execution.ID, "canceled") {
		t.Fatal("queued cancellation audit missing")
	}
	if err := manager.enqueue(request); !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown admission remained open")
	}
}

func awaitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(10 * time.Second):
		t.Fatal("operation did not reach synchronization point")
	}
}
