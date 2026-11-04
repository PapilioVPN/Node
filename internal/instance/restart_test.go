package instance_test

import (
	"context"
	"errors"
	"testing"

	"github.com/papilio-platform/Node/internal/instance"
)

func TestRestartReplacesOnlyAfterCleanup(t *testing.T) {
	old, replacement := newSession(), newSession()
	engine := boundStarter(old)
	service := newService(t, engine)
	if _, err := service.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	engine.start = func(context.Context) (instance.ISession, error) {
		if state := old.Observe(); !state.CleanupComplete || state.CleanupFailure != nil {
			t.Error("replacement started before previous cleanup")
		}
		return replacement, nil
	}
	result, err := service.Restart(t.Context())
	if err != nil || result.Current.Phase != instance.PhaseReady || result.Current.AppliedRevision != engine.revision || result.Previous.Process.Exit == nil || result.Previous.Phase != instance.PhaseStopped || result.StopFailure != nil || engine.calls.Load() != 2 || old.stops.Load() != 1 || replacement.stops.Load() != 0 {
		t.Fatalf("restart did not replace the owned execution: %+v, %v", result, err)
	}
	if _, err := service.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestRestartRecoversAbnormalExitWithoutHidingIt(t *testing.T) {
	old := newSession()
	nativeFailure := errors.New("native exit 42")
	old.stop = func(context.Context) error { return nativeFailure }
	engine := boundStarter(old)
	service := newService(t, engine)
	if _, err := service.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	old.observation.Exit = &instance.Exit{Code: 42, Cause: nativeFailure}
	old.observation.Failure = nativeFailure
	old.observation.CleanupComplete = true
	engine.session = newSession()
	result, err := service.Restart(t.Context())
	if err != nil || result.Current.Phase != instance.PhaseReady || result.Current.LastFailure != nil || !errors.Is(result.StopFailure, nativeFailure) || result.Previous.Process.Exit == nil || result.Previous.Process.Exit.Code != 42 || engine.calls.Load() != 2 {
		t.Fatalf("restart hid the old exit or failed to recover: %+v, %v", result, err)
	}
	if _, err := service.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestRestartBlocksReplacementOnIncompleteCleanup(t *testing.T) {
	old := newSession()
	old.cleanupErr = errors.New("owned files remain")
	engine := boundStarter(old)
	service := newService(t, engine)
	if _, err := service.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	result, err := service.Restart(t.Context())
	if !errors.Is(err, instance.ErrCleanupIncomplete) || !errors.Is(result.StopFailure, old.cleanupErr) || result.Current.Phase != instance.PhaseFailed || engine.calls.Load() != 1 || result.Current.AppliedRevision != engine.revision {
		t.Fatalf("restart overlapped unreleased ownership: %+v, %v", result, err)
	}
}

func TestRestartFailureRetainsAppliedHistoryAndCanRecover(t *testing.T) {
	engine := boundStarter(newSession())
	service := newService(t, engine)
	if _, err := service.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	rejected := errors.New("replacement rejected")
	engine.start = func(context.Context) (instance.ISession, error) { return nil, rejected }
	result, err := service.Restart(t.Context())
	if !errors.Is(err, rejected) || result.Current.Phase != instance.PhaseFailed || result.Current.AppliedRevision != engine.revision || result.Previous.Process.Exit == nil || result.Current.Process.PID != 0 {
		t.Fatalf("failed replacement fabricated success or lost history: %+v, %v", result, err)
	}
	engine.start = nil
	engine.session = newSession()
	result, err = service.Restart(t.Context())
	if err != nil || result.Current.Phase != instance.PhaseReady || engine.calls.Load() != 3 {
		t.Fatalf("explicit recovery failed: %+v, %v", result, err)
	}
	if _, err := service.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestRestartStoppedAndPrecancelledAdmission(t *testing.T) {
	engine := boundStarter(newSession())
	service := newService(t, engine)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.Restart(ctx); !errors.Is(err, context.Canceled) || engine.calls.Load() != 0 {
		t.Fatal("pre-cancelled restart executed engine work or reported success")
	}
	result, err := service.Restart(t.Context())
	if err != nil || result.Current.Phase != instance.PhaseReady || result.Previous.Phase != instance.PhaseStopped || result.Previous.AppliedRevision != "" || engine.calls.Load() != 1 {
		t.Fatalf("stopped restart did not start the binding: %+v, %v", result, err)
	}
	if _, err := service.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestRestartCancelledReplacementJoinsCandidateCleanup(t *testing.T) {
	old, replacement := newSession(), newSession()
	engine := boundStarter(old)
	service := newService(t, engine)
	if _, err := service.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	engine.start = func(context.Context) (instance.ISession, error) { cancel(); return replacement, nil }
	result, err := service.Restart(ctx)
	if !errors.Is(err, context.Canceled) || result.Current.Phase != instance.PhaseFailed || result.Current.AppliedRevision != engine.revision || !result.Current.Process.CleanupComplete || old.stops.Load() != 1 || replacement.stops.Load() != 1 {
		t.Fatalf("cancelled replacement remained active or erased history: %+v, %v", result, err)
	}
	if _, err := service.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestRestartCannotRetryUnconfirmedSourceCleanup(t *testing.T) {
	engine := boundStarter(newSession())
	service := newService(t, engine)
	if _, err := service.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	engine.start = func(context.Context) (instance.ISession, error) { return nil, instance.ErrCleanupIncomplete }
	for range 2 {
		result, err := service.Restart(t.Context())
		if !errors.Is(err, instance.ErrCleanupIncomplete) || result.Current.Phase != instance.PhaseFailed || engine.calls.Load() != 2 {
			t.Fatalf("unconfirmed cleanup allowed another execution: %+v, %v", result, err)
		}
	}
}
