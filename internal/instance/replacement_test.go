package instance_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/papilio-platform/Node/internal/instance"
)

func TestReplacementFencesAndRetainsAppliedHistory(t *testing.T) {
	old := boundStarter(newSession())
	s := newService(t, old)
	if _, err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginReplacement(t.Context(), "stale"); !errors.Is(err, instance.ErrRevisionConflict) {
		t.Fatalf("stale admission: %v", err)
	}
	r, err := s.BeginReplacement(t.Context(), old.revision)
	if err != nil {
		t.Fatal(err)
	}
	next := &starter{revision: "next", start: func(context.Context) (instance.ISession, error) { return nil, errors.New("rejected") }}
	result, err := r.Activate(next)
	current, closeErr := r.Close()
	if err == nil || closeErr != nil || result.Previous.Phase != instance.PhaseStopped || current.Phase != instance.PhaseFailed || current.DesiredRevision != "next" || current.AppliedRevision != old.revision || next.calls.Load() != 1 {
		t.Fatalf("replacement facts: %+v %+v %v %v", result, current, err, closeErr)
	}
	if _, err := s.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestReplacementAbortedPreflightPreservesLiveRun(t *testing.T) {
	engine := boundStarter(newSession())
	s := newService(t, engine)
	before, err := s.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.BeginReplacement(t.Context(), engine.revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(t.Context()); !errors.Is(err, instance.ErrBusy) {
		t.Fatal("reservation did not block start", err)
	}
	if _, err := s.Restart(t.Context()); !errors.Is(err, instance.ErrBusy) {
		t.Fatal("reservation did not block restart", err)
	}
	current, err := r.Close()
	if err != nil || current.Phase != instance.PhaseReady || current.Process.PID != before.Process.PID || engine.calls.Load() != 1 {
		t.Fatalf("aborted preflight disturbed engine: %+v %v", current, err)
	}
	r.Close()
	if _, err := s.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestStopCancelsAndJoinsReplacementPreflight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		engine := boundStarter(newSession())
		s := newService(t, engine)
		if _, err := s.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		r, err := s.BeginReplacement(t.Context(), engine.revision)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := s.Stop(t.Context()); done <- err }()
		synctest.Wait()
		if !errors.Is(r.Context().Err(), context.Canceled) {
			t.Fatal("Stop failed to cancel preflight")
		}
		select {
		case <-done:
			t.Fatal("Stop returned before preflight joined")
		default:
		}
		candidate := &starter{revision: "next", session: newSession()}
		_, err = r.Activate(candidate)
		current, closeErr := r.Close()
		if !errors.Is(err, context.Canceled) || !errors.Is(closeErr, context.Canceled) || current.Phase != instance.PhaseStopped || current.DesiredRevision != engine.revision || candidate.calls.Load() != 0 {
			t.Fatalf("interrupted preflight resurrected engine: %+v %v %v", current, err, closeErr)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestReplacementFailedCleanupKeepsOldBinding(t *testing.T) {
	old := newSession()
	old.cleanupErr = errors.New("unreleased")
	engine := boundStarter(old)
	s := newService(t, engine)
	s.Start(t.Context())
	r, err := s.BeginReplacement(t.Context(), engine.revision)
	if err != nil {
		t.Fatal(err)
	}
	next := &starter{revision: "next", session: newSession()}
	result, err := r.Activate(next)
	r.Close()
	if !errors.Is(err, instance.ErrCleanupIncomplete) || result.Current.DesiredRevision != engine.revision || next.calls.Load() != 0 {
		t.Fatalf("unreleased old binding replaced: %+v %v", result, err)
	}
}

func TestStopJoinsChangedReplacementAfterWaiterDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		old := boundStarter(newSession())
		s := newService(t, old)
		s.Start(t.Context())
		r, err := s.BeginReplacement(t.Context(), old.revision)
		if err != nil {
			t.Fatal(err)
		}
		late := newSession()
		release := make(chan struct{})
		next := &starter{revision: "changed", start: func(context.Context) (instance.ISession, error) { <-release; return late, nil }}
		done := make(chan error, 1)
		go func() { _, err := r.Activate(next); _, closeErr := r.Close(); done <- errors.Join(err, closeErr) }()
		synctest.Wait()
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if _, err := s.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("stop waiter deadline was ignored", err)
		}
		if _, err := s.BeginReplacement(t.Context(), "changed"); !errors.Is(err, instance.ErrBusy) {
			t.Fatal("expired stop waiter abandoned reservation", err)
		}
		close(release)
		stopped, err := s.Stop(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal("late replacement claimed success", err)
		}
		if stopped.Phase != instance.PhaseStopped || stopped.DesiredRevision != "changed" || stopped.AppliedRevision != old.revision || late.stops.Load() != 1 || !late.Observe().CleanupComplete {
			t.Fatalf("late changed execution escaped ownership: %+v", stopped)
		}
	})
}
