package integration_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/instance"
)

func TestInstanceObservesRealXrayExitAndRevision(t *testing.T) {
	launcher, _, _ := nativeLauncher(t,
		[]byte(`{"log":{"loglevel":"none"},"outbounds":[{"protocol":"freedom"}]}`))
	service, err := instance.New("primary", "xray", launcher)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := service.Stop(ctx)
		assertNativeShutdown(t, err)
	})
	initial := service.Status()
	if initial.Phase != instance.PhaseStopped || initial.AppliedRevision != "" || initial.DesiredRevision != launcher.Revision() {
		t.Fatalf("incorrect immutable binding: %+v", initial)
	}
	status, err := service.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Phase != instance.PhaseReady || status.Process.Version != "26.10.31" || status.AppliedRevision != initial.DesiredRevision || status.Process.ReadyAt.IsZero() || status.Process.VersionObservedAt.IsZero() || status.Process.StartedAt.IsZero() {
		t.Fatalf("native execution missing from application observation: %+v", status)
	}
	for range 100 {
		if observed := service.Status(); observed.Process.ReadyAt != status.Process.ReadyAt || observed.Process.PID != status.Process.PID {
			t.Fatal("status changed readiness without an observation")
		}
	}
	reused, err := service.Start(context.Background())
	if err != nil || reused.Process.PID != status.Process.PID || reused.AppliedRevision != status.AppliedRevision || reused.Process.ReadyAt.Before(status.Process.ReadyAt) {
		t.Fatalf("repeated start failed to probe/reuse the real session: %+v, %v", reused, err)
	}
	owned, err := os.FindProcess(status.Process.PID)
	if err != nil {
		t.Fatal(err)
	}
	if err := owned.Kill(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		status = service.Status()
		if status.Process.CleanupComplete {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("owned exit/cleanup was not observed")
		}
	}
	if status.Phase != instance.PhaseFailed || status.AppliedRevision != initial.DesiredRevision || !errors.Is(status.LastFailure, instance.ErrUnexpectedExit) || status.Process.Exit == nil || status.Process.Exit.Signal != int(syscall.SIGKILL) || status.Process.Exit.Code != -1 {
		t.Fatalf("unexpected native exit was hidden: %+v", status)
	}
	var exit *exec.ExitError
	if !errors.As(status.LastFailure, &exit) {
		t.Fatal("native exit cause lost")
	}
	copy := status.Process.Exit
	copy.Code = 12345
	if service.Status().Process.Exit.Code != -1 {
		t.Fatal("observation exposed mutable session state")
	}
	if _, err := service.Start(ctx); !errors.Is(err, instance.ErrFailed) {
		t.Fatal("observation triggered an implicit restart")
	}
	stopped, err := service.Stop(ctx)
	if stopped.Phase != instance.PhaseStopped || stopped.AppliedRevision != initial.DesiredRevision || !errors.As(err, &exit) {
		t.Fatalf("stop erased the historical failure: %+v, %v", stopped, err)
	}
}

func TestInstanceStopsRealXrayWithoutErasingAppliedRevision(t *testing.T) {
	launcher, _, _ := nativeLauncher(t,
		[]byte(`{"log":{"loglevel":"none"},"outbounds":[{"protocol":"freedom"}]}`))
	service, err := instance.New("primary", "xray", launcher)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := service.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := service.Stop(ctx)
	assertNativeShutdown(t, err)
	if stopped.Phase != instance.PhaseStopped || stopped.AppliedRevision != status.DesiredRevision || stopped.Process.Exit == nil || !stopped.Process.CleanupComplete {
		t.Fatalf("stop did not preserve real execution history: %+v", stopped)
	}
}

func BenchmarkInstanceStatus(b *testing.B) {
	launcher, _, _ := nativeLauncher(b,
		[]byte(`{"log":{"loglevel":"none"},"outbounds":[{"protocol":"freedom"}]}`))
	service, err := instance.New("primary", "xray", launcher)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := service.Start(b.Context()); err != nil {
		b.Fatal(err)
	}
	defer func() {
		if _, err := service.Stop(context.Background()); err != nil {
			b.Error(err)
		}
	}()
	b.ReportAllocs()
	for b.Loop() {
		service.Status()
	}
}
