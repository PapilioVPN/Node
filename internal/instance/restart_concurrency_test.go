package instance_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/papilio-platform/Node/internal/instance"
)

func TestStopInterruptsBothRestartPhases(t *testing.T) {
	for _, phase := range []string{"old-stop", "replacement-start"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				old, replacement := newSession(), newSession()
				engine := boundStarter(old)
				service := newService(t, engine)
				if _, err := service.Start(t.Context()); err != nil {
					t.Fatal(err)
				}
				release := make(chan struct{})
				var once sync.Once
				releaseWork := func() { once.Do(func() { close(release) }) }
				defer releaseWork()
				if phase == "old-stop" {
					old.stop = func(context.Context) error { <-release; return nil }
				}
				engine.start = func(ctx context.Context) (instance.ISession, error) {
					if phase == "replacement-start" {
						<-ctx.Done()
						<-release
					}
					return replacement, nil
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan error, 1)
				go func() { _, err := service.Restart(ctx); done <- err }()
				synctest.Wait()
				if _, err := service.Start(t.Context()); !errors.Is(err, instance.ErrBusy) {
					t.Error("start entered the reserved restart")
				}
				if _, err := service.Restart(t.Context()); !errors.Is(err, instance.ErrBusy) {
					t.Error("second restart entered the reserved transition")
				}
				waiter, cancelWaiter := context.WithCancel(t.Context())
				waiterDone := make(chan error, 1)
				go func() { _, err := service.Stop(waiter); waiterDone <- err }()
				synctest.Wait()
				if status := service.Status(); status.Phase != instance.PhaseStopping {
					t.Errorf("accepted Stop did not report stopping: %+v", status)
				}
				cancelWaiter()
				if err := <-waiterDone; !errors.Is(err, context.Canceled) {
					t.Error("cancelled stop waiter lost cancellation")
				}
				const callers = 8
				results := make(chan error, callers)
				var group sync.WaitGroup
				for range callers {
					group.Go(func() { _, err := service.Stop(t.Context()); results <- err })
				}
				synctest.Wait()
				releaseWork()
				group.Wait()
				for range callers {
					if err := <-results; err != nil {
						t.Errorf("joined restart cleanup failed: %v", err)
					}
				}
				if err := <-done; !errors.Is(err, context.Canceled) {
					t.Errorf("interrupted restart reported success: %v", err)
				}
				wantCalls, wantCleanup := int64(1), int64(0)
				if phase == "replacement-start" {
					wantCalls, wantCleanup = 2, 1
				}
				if status := service.Status(); status.Phase != instance.PhaseStopped || status.AppliedRevision != engine.revision || engine.calls.Load() != wantCalls || old.stops.Load() != 1 || replacement.stops.Load() != wantCleanup {
					t.Fatalf("interrupted restart leaked or resurrected an execution: %+v", status)
				}
			})
		})
	}
}

func TestRestartDeadlineDuringStopJoinsCleanupWithoutReplacement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		old := newSession()
		engine := boundStarter(old)
		service := newService(t, engine)
		if _, err := service.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		release := make(chan struct{})
		old.stop = func(context.Context) error { <-release; return nil }
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := service.Restart(ctx); done <- err }()
		synctest.Wait()
		<-ctx.Done()
		synctest.Wait()
		if service.Status().Phase != instance.PhaseStopping || len(done) != 0 {
			t.Error("restart abandoned old cleanup after its deadline")
		}
		close(release)
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) || engine.calls.Load() != 1 || service.Status().Phase != instance.PhaseStopped {
			t.Fatalf("expired restart admitted a replacement: %v", err)
		}
	})
}

func TestRestartRejectsAnActiveReuseProbe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		execution := newSession()
		execution.probe = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
		engine := boundStarter(execution)
		service := newService(t, engine)
		if _, err := service.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := service.Start(ctx); done <- err }()
		synctest.Wait()
		if _, err := service.Restart(t.Context()); !errors.Is(err, instance.ErrBusy) || execution.stops.Load() != 0 || engine.calls.Load() != 1 {
			t.Error("restart overlapped an admitted reuse probe")
		}
		cancel()
		<-done
		if _, err := service.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestMixedLifecycleOperationsKeepOneOwner(t *testing.T) {
	var active, released atomic.Int64
	engine := boundStarter(nil)
	engine.start = func(context.Context) (instance.ISession, error) {
		if count := active.Add(1); count != 1 {
			t.Errorf("%d simultaneous owned executions", count)
		}
		execution := newSession()
		var once sync.Once
		execution.stop = func(context.Context) error {
			once.Do(func() { active.Add(-1); released.Add(1) })
			return nil
		}
		return execution, nil
	}
	service := newService(t, engine)
	var group sync.WaitGroup
	for worker := range 16 {
		group.Go(func() {
			for step := range 10 {
				var err error
				switch (worker + step) % 3 {
				case 0:
					_, err = service.Start(t.Context())
				case 1:
					_, err = service.Restart(t.Context())
				case 2:
					_, err = service.Stop(t.Context())
				}
				if err != nil && !errors.Is(err, instance.ErrBusy) && !errors.Is(err, context.Canceled) {
					t.Errorf("unexpected contended lifecycle failure: %v", err)
				}
			}
		})
	}
	group.Wait()
	if _, err := service.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if active.Load() != 0 || released.Load() != engine.calls.Load() || service.Status().Phase != instance.PhaseStopped {
		t.Fatalf("owned executions leaked: active %d, released %d, created %d", active.Load(), released.Load(), engine.calls.Load())
	}
}

func TestRestartCancellationPreservesCleanupFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		old := newSession()
		old.cleanupErr = errors.New("owned directory remains")
		engine := boundStarter(old)
		service := newService(t, engine)
		if _, err := service.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		release := make(chan struct{})
		old.stop = func(context.Context) error { <-release; return nil }
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := service.Restart(ctx); done <- err }()
		synctest.Wait()
		cancel()
		close(release)
		if err := <-done; !errors.Is(err, context.Canceled) || !errors.Is(err, instance.ErrCleanupIncomplete) || !errors.Is(err, old.cleanupErr) || engine.calls.Load() != 1 {
			t.Fatalf("cancellation or owned cleanup failure was hidden: %v", err)
		}
	})
}

func TestRestartRejectsOtherActiveLifecycleWork(t *testing.T) {
	for _, work := range []string{"start", "stop"} {
		t.Run(work, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				execution := newSession()
				engine := boundStarter(execution)
				service := newService(t, engine)
				release := make(chan struct{})
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if work == "start" {
					engine.start = func(ctx context.Context) (instance.ISession, error) {
						<-release
						return nil, ctx.Err()
					}
				} else {
					if _, err := service.Start(t.Context()); err != nil {
						t.Fatal(err)
					}
					execution.stop = func(context.Context) error { <-release; return nil }
				}
				done := make(chan error, 1)
				go func() {
					var err error
					if work == "start" {
						_, err = service.Start(ctx)
					} else {
						_, err = service.Stop(ctx)
					}
					done <- err
				}()
				synctest.Wait()
				if _, err := service.Restart(t.Context()); !errors.Is(err, instance.ErrBusy) || engine.calls.Load() != 1 {
					t.Error("restart overlapped another lifecycle owner")
				}
				cancel()
				close(release)
				<-done
				if _, err := service.Stop(t.Context()); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}
