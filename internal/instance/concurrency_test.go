package instance_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/papilio-platform/Node/internal/instance"
)

func TestConcurrentReadyStartsBoundNativeProbes(t *testing.T) {
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
		first := make(chan error, 1)
		go func() { _, err := service.Start(ctx); first <- err }()
		synctest.Wait()

		const callers = 32
		results := make(chan error, callers)
		var group sync.WaitGroup
		for range callers {
			group.Go(func() { _, err := service.Start(ctx); results <- err })
		}
		synctest.Wait()
		if calls := execution.probes.Load(); calls != 1 {
			t.Errorf("contended ready starts issued %d probes; want one", calls)
		}
		if status := service.Status(); status.Phase != instance.PhaseReady || engine.calls.Load() != 1 {
			t.Errorf("reuse changed the owned execution: %+v", status)
		}
		cancel()
		group.Wait()
		for range callers {
			if err := <-results; !errors.Is(err, instance.ErrBusy) {
				t.Errorf("contending start did not reject admission: %v", err)
			}
		}
		if err := <-first; !errors.Is(err, context.Canceled) {
			t.Errorf("admitted probe lost cancellation: %v", err)
		}
		if _, err := service.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestStopCancelsAndJoinsReuseProbe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		execution := newSession()
		cancelled, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		releaseProbe := func() { once.Do(func() { close(release) }) }
		defer releaseProbe()
		staleFailure := errors.New("old API response")
		execution.probe = func(ctx context.Context) error {
			<-ctx.Done()
			close(cancelled)
			<-release
			return staleFailure
		}
		engine := boundStarter(execution)
		service := newService(t, engine)
		if _, err := service.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		probeDone := make(chan error, 1)
		go func() { _, err := service.Start(ctx); probeDone <- err }()
		synctest.Wait()
		stopDone := make(chan struct{})
		var stopErr error
		stopCtx, cancelStop := context.WithCancel(t.Context())
		defer cancelStop()
		go func() { _, stopErr = service.Stop(stopCtx); close(stopDone) }()
		synctest.Wait()
		select {
		case <-cancelled:
		default:
			t.Error("Stop did not cancel the admitted probe")
		}
		select {
		case <-stopDone:
			t.Error("Stop returned before its admitted probe joined")
		default:
		}
		if status := service.Status(); status.Phase != instance.PhaseStopping {
			t.Errorf("cleanup admitted a replacement too early: %+v", status)
		}
		if _, err := service.Start(t.Context()); !errors.Is(err, instance.ErrBusy) {
			t.Errorf("start overlapped probe cleanup: %v", err)
		}
		cancelStop()
		<-stopDone
		if !errors.Is(stopErr, context.Canceled) || service.Status().Phase != instance.PhaseStopping {
			t.Errorf("cancelled waiter abandoned probe cleanup: %v", stopErr)
		}
		joined := make(chan error, 1)
		go func() { _, err := service.Stop(t.Context()); joined <- err }()
		synctest.Wait()
		cancel()
		releaseProbe()
		if err := <-joined; err != nil {
			t.Fatal(err)
		}
		if err := <-probeDone; !errors.Is(err, instance.ErrBusy) || !errors.Is(err, staleFailure) || !errors.Is(err, context.Canceled) {
			t.Errorf("stale probe result lost conflict/cause: %v", err)
		}
		if status := service.Status(); status.Phase != instance.PhaseStopped || status.LastFailure != nil || execution.stops.Load() != 1 {
			t.Fatalf("stale probe changed completed shutdown: %+v", status)
		}
		engine.session = newSession()
		status, err := service.Start(t.Context())
		if err != nil || status.Phase != instance.PhaseReady || status.LastFailure != nil || engine.calls.Load() != 2 {
			t.Fatalf("old probe poisoned the next execution: %+v, %v", status, err)
		}
		if _, err := service.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCancelledReuseCannotReportSuccess(t *testing.T) {
	execution := newSession()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	execution.probe = func(context.Context) error { cancel(); return nil }
	service := newService(t, boundStarter(execution))
	if _, err := service.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err := service.Start(ctx)
	if !errors.Is(err, context.Canceled) || status.Phase != instance.PhaseReady || status.LastFailure != nil {
		t.Fatalf("cancelled caller reported successful reuse or damaged readiness: %+v, %v", status, err)
	}
	if _, err := service.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestInterruptedStartupHasOneCleanupOwner(t *testing.T) {
	for _, cleanupFailure := range []bool{false, true} {
		name := "released"
		if cleanupFailure {
			name = "cleanup-failed"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				execution := newSession()
				if cleanupFailure {
					execution.cleanupErr = errors.New("owned files remain")
				}
				release := make(chan struct{})
				var once sync.Once
				returnSession := func() { once.Do(func() { close(release) }) }
				defer returnSession()
				engine := boundStarter(execution)
				engine.start = func(ctx context.Context) (instance.ISession, error) {
					<-ctx.Done()
					<-release
					return execution, nil
				}
				service := newService(t, engine)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				startDone := make(chan error, 1)
				go func() { _, err := service.Start(ctx); startDone <- err }()
				synctest.Wait()
				const callers = 16
				results := make(chan error, callers)
				var group sync.WaitGroup
				for range callers {
					group.Go(func() { _, err := service.Stop(t.Context()); results <- err })
				}
				synctest.Wait()
				if status := service.Status(); status.Phase != instance.PhaseStopping || status.AppliedRevision != "" || execution.stops.Load() != 0 {
					t.Errorf("interrupted startup committed or cleaned before receiving its session: %+v", status)
				}
				if _, err := service.Start(t.Context()); !errors.Is(err, instance.ErrBusy) || engine.calls.Load() != 1 {
					t.Errorf("replacement overlapped startup: %v", err)
				}
				waiter, cancelWaiter := context.WithCancel(t.Context())
				waiterDone := make(chan error, 1)
				go func() { _, err := service.Stop(waiter); waiterDone <- err }()
				synctest.Wait()
				cancelWaiter()
				if err := <-waiterDone; !errors.Is(err, context.Canceled) {
					t.Errorf("cancelled stop waiter lost cancellation: %v", err)
				}
				returnSession()
				group.Wait()
				for range callers {
					err := <-results
					if cleanupFailure && !errors.Is(err, instance.ErrCleanupIncomplete) {
						t.Errorf("startup cleanup failure hidden from stop waiter: %v", err)
					} else if !cleanupFailure && err != nil {
						t.Errorf("completed startup cleanup failed: %v", err)
					}
				}
				if err := <-startDone; !errors.Is(err, context.Canceled) {
					t.Errorf("late session was accepted after stop: %v", err)
				}
				want := instance.PhaseStopped
				if cleanupFailure {
					want = instance.PhaseFailed
				}
				if status := service.Status(); status.Phase != want || status.AppliedRevision != "" || execution.stops.Load() != 1 {
					t.Fatalf("startup cleanup ownership lost: %+v, calls %d", status, execution.stops.Load())
				}
			})
		})
	}
}

func TestConcurrentStopsJoinOneOutcome(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		execution := newSession()
		release := make(chan struct{})
		var once sync.Once
		releaseStop := func() { once.Do(func() { close(release) }) }
		defer releaseStop()
		nativeFailure := errors.New("native signal exit")
		execution.stop = func(context.Context) error { <-release; return nativeFailure }
		engine := boundStarter(execution)
		service := newService(t, engine)
		if _, err := service.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		const callers = 16
		results := make(chan error, callers)
		var group sync.WaitGroup
		for range callers {
			group.Go(func() { _, err := service.Stop(t.Context()); results <- err })
		}
		synctest.Wait()
		if execution.stops.Load() != 1 || service.Status().Phase != instance.PhaseStopping {
			t.Error("concurrent stops did not share one cleanup owner")
		}
		if _, err := service.Start(t.Context()); !errors.Is(err, instance.ErrBusy) || engine.calls.Load() != 1 {
			t.Errorf("start overlapped owned shutdown: %v", err)
		}
		releaseStop()
		group.Wait()
		for range callers {
			if err := <-results; !errors.Is(err, nativeFailure) {
				t.Errorf("joined waiter lost native shutdown outcome: %v", err)
			}
		}
		if status := service.Status(); status.Phase != instance.PhaseStopped || !errors.Is(status.LastFailure, nativeFailure) {
			t.Fatalf("abnormal exit became graceful success: %+v", status)
		}
	})
}

func TestPrecancelledLifecycleDoesNoEngineWork(t *testing.T) {
	execution := newSession()
	engine := boundStarter(execution)
	service := newService(t, engine)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.Start(ctx); !errors.Is(err, context.Canceled) || engine.calls.Load() != 0 {
		t.Fatal("pre-cancelled start executed engine work")
	}
	if _, err := service.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(ctx); !errors.Is(err, context.Canceled) || execution.probes.Load() != 0 {
		t.Fatal("pre-cancelled reuse executed a probe")
	}
	status, err := service.Stop(ctx)
	if !errors.Is(err, context.Canceled) || execution.stops.Load() != 0 || status.Phase != instance.PhaseReady {
		t.Fatal("pre-cancelled stop changed a running instance")
	}
	if _, err := service.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestInstanceAdmissionIsIndependent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		slowEngine := boundStarter(nil)
		slowEngine.start = func(ctx context.Context) (instance.ISession, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		slow := newService(t, slowEngine)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := slow.Start(ctx); done <- err }()
		synctest.Wait()
		independent := newService(t, boundStarter(newSession()))
		if _, err := independent.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := independent.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		if slow.Status().Phase != instance.PhaseStarting {
			t.Error("independent operation changed the slow instance")
		}
		if _, err := slow.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal("slow startup did not join cancellation")
		}
	})
}
