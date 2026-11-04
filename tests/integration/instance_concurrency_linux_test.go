package integration_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/instance"
)

// Hold the real ready session at the application admission boundary.
type delayedNativeStarter struct {
	instance.IStarter
	ready   chan instance.ISession
	release <-chan struct{}
	calls   atomic.Int64
}

func (s *delayedNativeStarter) Start(ctx context.Context) (instance.ISession, error) {
	s.calls.Add(1)
	session, err := s.IStarter.Start(ctx)
	if err == nil {
		s.ready <- session
		<-s.release
	}
	return session, err
}

func TestConcurrentStopRejectsLateRealXrayStartup(t *testing.T) {
	launcher, _, root := nativeLauncher(t,
		[]byte(`{"log":{"loglevel":"none"},"outbounds":[{"protocol":"freedom"}]}`))
	release := make(chan struct{})
	var once sync.Once
	releaseStartup := func() { once.Do(func() { close(release) }) }
	starter := &delayedNativeStarter{IStarter: launcher, ready: make(chan instance.ISession, 1), release: release}
	service, err := instance.New("primary", "xray", starter)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	var group sync.WaitGroup
	t.Cleanup(func() {
		releaseStartup()
		cancel()
		group.Wait()
		ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, err := service.Stop(ctx)
		assertNativeShutdown(t, err)
	})
	startDone := make(chan error, 1)
	group.Go(func() { _, err := service.Start(ctx); startDone <- err })
	var owned instance.ISession
	select {
	case owned = <-starter.ready:
	case <-ctx.Done():
		t.Fatal("real startup never reached native API readiness")
	}
	if err := owned.Probe(ctx); err != nil {
		t.Fatal(err)
	}
	const callers = 12
	results := make(chan error, callers)
	for range callers {
		group.Go(func() { _, err := service.Stop(ctx); results <- err })
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for service.Status().Phase != instance.PhaseStopping {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("Stop did not interrupt the pending startup")
		}
	}
	if _, err := service.Start(ctx); !errors.Is(err, instance.ErrBusy) || starter.calls.Load() != 1 {
		t.Fatal("conflicting start admitted a second native process")
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one owned native runtime before cleanup: %v, %v", entries, err)
	}
	releaseStartup()
	group.Wait()
	if err := <-startDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted real startup reported success: %v", err)
	}
	for range callers {
		assertNativeShutdown(t, <-results)
	}
	status := service.Status()
	if status.Phase != instance.PhaseStopped || status.AppliedRevision != "" || status.Process.Exit == nil || !status.Process.CleanupComplete || status.Process.CleanupFailure != nil {
		t.Fatalf("late real session committed or leaked: %+v", status)
	}
	if err := syscall.Kill(owned.PID(), 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("owned native child still exists after interruption: %v", err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("interrupted startup retained native socket/configuration: %v, %v", entries, err)
	}
	restarted, err := service.Start(ctx)
	if err != nil || restarted.Phase != instance.PhaseReady || restarted.AppliedRevision != launcher.Revision() || starter.calls.Load() != 2 {
		t.Fatalf("explicit restart did not recover the native instance: %+v, %v", restarted, err)
	}
	_, err = service.Stop(ctx)
	assertNativeShutdown(t, err)
}
