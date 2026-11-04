package instance_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/papilio-platform/Node/internal/instance"
)

func TestAccessFencesExecutionAndReservesNativeOwner(t *testing.T) {
	engine := boundStarter(newSession())
	s := newService(t, engine)
	if _, err := s.Acquire(t.Context(), 0); !errors.Is(err, instance.ErrNotReady) {
		t.Fatal("stopped access", err)
	}
	status, err := s.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if status.Generation == 0 {
		t.Fatal("missing execution generation")
	}
	if _, err := s.Acquire(t.Context(), status.Generation+1); !errors.Is(err, instance.ErrExecutionConflict) {
		t.Fatal("stale access", err)
	}
	a, err := s.Acquire(t.Context(), status.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if a.Session() != engine.session {
		t.Fatal("different native owner")
	}
	if _, err := s.Acquire(t.Context(), status.Generation); !errors.Is(err, instance.ErrBusy) {
		t.Fatal("unbounded access", err)
	}
	if _, err := s.Start(t.Context()); !errors.Is(err, instance.ErrBusy) {
		t.Fatal("probe overlapped access", err)
	}
	if _, err := s.Restart(t.Context()); !errors.Is(err, instance.ErrBusy) {
		t.Fatal("restart overlapped access", err)
	}
	if _, err := s.BeginReplacement(t.Context(), engine.revision); !errors.Is(err, instance.ErrBusy) {
		t.Fatal("rebind overlapped access", err)
	}
	a.Close()
	a.Close()
	engine.session = newSession()
	replaced, err := s.Restart(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Current.Generation == status.Generation {
		t.Fatal("restart reused target generation")
	}
	if _, err := s.Acquire(t.Context(), status.Generation); !errors.Is(err, instance.ErrExecutionConflict) {
		t.Fatal("old target entered new engine", err)
	}
	s.Stop(t.Context())
}

func TestStopCancelsAndJoinsAccessBeyondWaiterDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newService(t, boundStarter(newSession()))
		status, err := s.Start(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		a, err := s.Acquire(t.Context(), status.Generation)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		stopped, err := s.Stop(ctx)
		if !errors.Is(err, context.DeadlineExceeded) || stopped.Phase != instance.PhaseStopping || !errors.Is(a.Context().Err(), context.Canceled) {
			t.Fatalf("access cleanup abandoned: %+v %v", stopped, err)
		}
		if _, err := s.Start(t.Context()); !errors.Is(err, instance.ErrBusy) {
			t.Fatal("started before access joined", err)
		}
		a.Close()
		stopped, err = s.Stop(t.Context())
		if err != nil || stopped.Phase != instance.PhaseStopped {
			t.Fatalf("joined stop: %+v %v", stopped, err)
		}
	})
}

func TestAccessCancellationDoesNotStopExecution(t *testing.T) {
	s := newService(t, boundStarter(newSession()))
	status, err := s.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	a, err := s.Acquire(ctx, status.Generation)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	a.Close()
	if s.Status().Phase != instance.PhaseReady {
		t.Fatal("cancelled read stopped engine")
	}
	if _, err := s.Acquire(ctx, status.Generation); !errors.Is(err, context.Canceled) {
		t.Fatal("pre-cancelled admission", err)
	}
	s.Stop(t.Context())
}
