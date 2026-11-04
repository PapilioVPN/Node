package integration_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/instance"
)

func TestInstanceRestartsRealXrayAndRecoversAfterCrash(t *testing.T) {
	service, _, root := nativeInstance(t,
		[]byte(`{"log":{"loglevel":"none"},"outbounds":[{"protocol":"freedom"}]}`))
	current, err := service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, crash := range []bool{false, true} {
		if crash {
			owned, err := os.FindProcess(current.Process.PID)
			if err != nil {
				t.Fatal(err)
			}
			if err := owned.Kill(); err != nil {
				t.Fatal(err)
			}
			// Joining the actual child is part of Restart, including an in-flight exit.
		}
		result, err := service.Restart(t.Context())
		if err != nil || result.Current.Phase != instance.PhaseReady || result.Current.AppliedRevision != current.AppliedRevision || result.Previous.Process.PID != current.Process.PID || !result.Current.Process.StartedAt.After(current.Process.StartedAt) || result.Current.Process.Version != "26.10.31" {
			t.Fatalf("native restart did not admit a fresh ready execution: %+v, %v", result, err)
		}
		assertInstanceReleased(t, result.Previous)
		if crash {
			var exit *exec.ExitError
			if !errors.As(result.StopFailure, &exit) || result.Previous.Process.Exit.Signal != int(syscall.SIGKILL) || result.Previous.Process.Exit.Code != -1 {
				t.Fatalf("native recovery hid the previous SIGKILL: %+v", result)
			}
		} else {
			assertNativeShutdown(t, result.StopFailure)
		}
		if entries, err := os.ReadDir(root); err != nil || len(entries) != 1 {
			t.Fatalf("restart leaked previous runtime directories: %v, %v", entries, err)
		}
		current = result.Current
	}
	stopped, err := service.Stop(t.Context())
	assertNativeShutdown(t, err)
	assertInstanceReleased(t, stopped)
}

func TestStopInterruptsRealRestartBeforeReplacementCommit(t *testing.T) {
	launcher, _, root := nativeLauncher(t,
		[]byte(`{"log":{"loglevel":"none"},"outbounds":[{"protocol":"freedom"}]}`))
	release := make(chan struct{}, 1)
	release <- struct{}{} // Initial startup passes; replacement waits for release.
	var once sync.Once
	releaseReplacement := func() { once.Do(func() { close(release) }) }
	starter := &delayedNativeStarter{IStarter: launcher, ready: make(chan instance.ISession, 1), release: release}
	service, err := instance.New("primary", "xray", starter)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	var group sync.WaitGroup
	t.Cleanup(func() {
		releaseReplacement()
		cancel()
		group.Wait()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := service.Stop(ctx)
		assertNativeShutdown(t, err)
	})
	initial, err := service.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	<-starter.ready
	done := make(chan error, 1)
	group.Go(func() { _, err := service.Restart(ctx); done <- err })
	select {
	case <-starter.ready:
	case <-ctx.Done():
		t.Fatal("replacement did not reach real native readiness")
	}
	stopped := make(chan error, 1)
	group.Go(func() { _, err := service.Stop(ctx); stopped <- err })
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for service.Status().Phase != instance.PhaseStopping {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("Stop did not interrupt replacement admission")
		}
	}
	releaseReplacement()
	group.Wait()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted restart reported success: %v", err)
	}
	assertNativeShutdown(t, <-stopped)
	status := service.Status()
	assertInstanceReleased(t, status)
	if status.AppliedRevision != initial.AppliedRevision || starter.calls.Load() != 2 {
		t.Fatal("interrupted restart erased applied history or resurrected an engine")
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("interrupted restart retained owned runtime files: %v, %v", entries, err)
	}
}

func BenchmarkInstanceRestart(b *testing.B) {
	service, _, _ := nativeInstance(b,
		[]byte(`{"log":{"loglevel":"none"},"outbounds":[{"protocol":"freedom"}]}`))
	if _, err := service.Start(b.Context()); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		result, err := service.Restart(b.Context())
		if err != nil || result.StopFailure != nil || result.Current.Phase != instance.PhaseReady || !result.Previous.Process.CleanupComplete {
			b.Fatalf("native restart failed clean recovery: %+v, %v", result, err)
		}
	}
}
