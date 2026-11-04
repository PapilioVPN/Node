package configuration_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/papilio-platform/Node/internal/instance"
)

func TestStopJoinsApplyValidationAndBlocksLateReplacement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, life, old, f := fixture(t)
		life.Start(t.Context())
		f.next.check = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
		applyDone := make(chan error, 1)
		go func() { _, err := s.Apply(t.Context(), "old", []byte("next")); applyDone <- err }()
		synctest.Wait()
		if _, err := s.Read(); !errors.Is(err, instance.ErrBusy) {
			t.Fatal("read exposed transitional source", err)
		}
		if _, err := s.Validate(t.Context(), []byte("other")); !errors.Is(err, instance.ErrBusy) {
			t.Fatal("unbounded concurrent validation", err)
		}
		if _, err := s.Apply(t.Context(), "old", []byte("other")); !errors.Is(err, instance.ErrBusy) {
			t.Fatal("queued conflicting apply", err)
		}
		stopped, err := life.Stop(t.Context())
		if err != nil || stopped.Phase != instance.PhaseStopped {
			t.Fatalf("Stop did not join preflight: %+v %v", stopped, err)
		}
		if err := <-applyDone; !errors.Is(err, context.Canceled) {
			t.Fatal("apply hid stop interruption", err)
		}
		view, err := s.Read()
		if err != nil || string(view.Source) != "old" || old.stops != 1 || f.next.starts != 0 {
			t.Fatalf("late validation resurrected engine: %+v %v", view, err)
		}
	})
}

func TestValidationDeadlineLeavesReadyEngine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, life, old, f := fixture(t)
		life.Start(t.Context())
		f.next.check = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		result, err := s.Apply(ctx, "old", []byte("next"))
		if !errors.Is(err, context.DeadlineExceeded) || result.Current.Phase != instance.PhaseReady || old.stops != 0 {
			t.Fatalf("deadline destroyed old execution: %+v %v", result, err)
		}
		if _, err := s.Read(); err != nil {
			t.Fatal("admission leaked after deadline", err)
		}
	})
}
