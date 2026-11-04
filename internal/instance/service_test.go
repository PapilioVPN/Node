package instance_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/instance"
)

type starter struct {
	revision instance.Revision
	session  instance.ISession
	start    func(context.Context) (instance.ISession, error)
	calls    atomic.Int64
}

func (s *starter) Revision() instance.Revision { return s.revision }
func (s *starter) Start(ctx context.Context) (instance.ISession, error) {
	s.calls.Add(1)
	if s.start != nil {
		return s.start(ctx)
	}
	return s.session, nil
}

type session struct {
	mu          sync.Mutex
	observation instance.ProcessObservation
	probes      atomic.Int64
	stops       atomic.Int64
	probeErr    error
	probe       func(context.Context) error
	cleanupErr  error
	stop        func(context.Context) error
}

func newSession() *session {
	return &session{observation: instance.ProcessObservation{
		PID: 123, Version: "26.10.31", ReadyAt: time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC),
	}}
}
func (s *session) PID() int { return 123 }
func (s *session) Observe() instance.ProcessObservation {
	s.mu.Lock()
	defer s.mu.Unlock()
	observation := s.observation
	if observation.Exit != nil {
		exit := *observation.Exit
		observation.Exit = &exit
	}
	return observation
}
func (s *session) Probe(ctx context.Context) error {
	s.probes.Add(1)
	if s.probe != nil {
		return s.probe(ctx)
	}
	return s.probeErr
}
func (s *session) Stop(ctx context.Context) error {
	s.stops.Add(1)
	var err error
	if s.stop != nil {
		err = s.stop(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.observation.Exit == nil {
		s.observation.Exit = &instance.Exit{At: time.Now(), Code: 0}
	}
	s.observation.CleanupComplete = true
	s.observation.CleanupFailure = s.cleanupErr
	s.observation.Failure = errors.Join(err, s.cleanupErr)
	return s.observation.Failure
}
func (s *session) Wait(context.Context) error { return s.Observe().Failure }

func newService(t testing.TB, engine *starter) *instance.Service {
	t.Helper()
	service, err := instance.New("primary", "xray", engine)
	if err != nil {
		t.Fatal(err)
	}
	return service
}
func boundStarter(session instance.ISession) *starter {
	return &starter{revision: "captured-revision", session: session}
}

func TestStartCommitsObservedRevision(t *testing.T) {
	execution := newSession()
	engine := boundStarter(execution)
	service := newService(t, engine)
	initial := service.Status()
	if initial.Phase != instance.PhaseStopped || initial.DesiredRevision != engine.revision || initial.AppliedRevision != "" || engine.calls.Load() != 0 {
		t.Fatalf("incorrect initial binding: %+v", initial)
	}
	status, err := service.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Phase != instance.PhaseReady || status.AppliedRevision != engine.revision || status.Process.Version != "26.10.31" {
		t.Fatalf("successful native start was not reflected in status: %+v", status)
	}
	for range 100 {
		observed := service.Status()
		if observed.Process.ReadyAt != status.Process.ReadyAt {
			t.Fatal("status refreshed API readiness without probing")
		}
	}
	if engine.calls.Load() != 1 || execution.probes.Load() != 0 {
		t.Fatal("status caused extra engine I/O")
	}
	if _, err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if engine.calls.Load() != 1 || execution.probes.Load() != 1 {
		t.Fatal("repeated start failed to probe/reuse the session")
	}
}

func TestUnexpectedExitPreservesAppliedHistory(t *testing.T) {
	for _, code := range []int{0, 42} {
		execution := newSession()
		engine := boundStarter(execution)
		service := newService(t, engine)
		if _, err := service.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		execution.mu.Lock()
		execution.observation.Exit = &instance.Exit{At: time.Now(), Code: code}
		execution.observation.CleanupComplete = true
		execution.mu.Unlock()
		status := service.Status()
		if status.Phase != instance.PhaseFailed || !errors.Is(status.LastFailure, instance.ErrUnexpectedExit) || status.AppliedRevision != engine.revision || status.Process.Exit.Code != code {
			t.Fatalf("exit erased history or claimed readiness: %+v", status)
		}
		if _, err := service.Start(context.Background()); !errors.Is(err, instance.ErrFailed) || engine.calls.Load() != 1 {
			t.Fatal("unexpected exit caused an implicit restart")
		}
		stopped, err := service.Stop(context.Background())
		if err != nil || stopped.Phase != instance.PhaseStopped || !errors.Is(stopped.LastFailure, instance.ErrUnexpectedExit) {
			t.Fatalf("stop lost the observed failure: %+v, %v", stopped, err)
		}
	}
}

func TestFailedStartDoesNotApplyRevision(t *testing.T) {
	rejected := errors.New("configuration rejected")
	for _, incomplete := range []bool{false, true} {
		engine := boundStarter(nil)
		failure := rejected
		if incomplete {
			failure = errors.Join(rejected, instance.ErrCleanupIncomplete)
		}
		engine.start = func(context.Context) (instance.ISession, error) { return nil, failure }
		service := newService(t, engine)
		status, err := service.Start(context.Background())
		if !errors.Is(err, rejected) || status.Phase != instance.PhaseFailed || status.AppliedRevision != "" {
			t.Fatalf("failed start was applied: %+v, %v", status, err)
		}
		status, err = service.Stop(context.Background())
		if incomplete {
			if status.Phase != instance.PhaseFailed || !errors.Is(err, instance.ErrCleanupIncomplete) {
				t.Fatal("incomplete startup cleanup reported stopped")
			}
		} else if err != nil || status.Phase != instance.PhaseStopped {
			t.Fatal("failed start could not be acknowledged")
		}
	}
}

func TestLaterFailedStartRetainsLastAppliedRevision(t *testing.T) {
	engine := boundStarter(newSession())
	service := newService(t, engine)
	if _, err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	engine.start = func(context.Context) (instance.ISession, error) { return nil, errors.New("executable unavailable") }
	status, err := service.Start(context.Background())
	if err == nil || status.Phase != instance.PhaseFailed || status.AppliedRevision != engine.revision {
		t.Fatalf("later failure erased the committed revision: %+v, %v", status, err)
	}
}

func TestCleanupFailureDoesNotReportStopped(t *testing.T) {
	execution := newSession()
	execution.cleanupErr = errors.New("runtime directory could not be removed")
	service := newService(t, boundStarter(execution))
	if _, err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err := service.Stop(context.Background())
	if !errors.Is(err, instance.ErrCleanupIncomplete) || status.Phase != instance.PhaseFailed || status.Process.Exit == nil || status.AppliedRevision == "" {
		t.Fatalf("cleanup failure hidden: %+v, %v", status, err)
	}
}

func TestFailedReuseProbeDoesNotRestartLiveProcess(t *testing.T) {
	execution := newSession()
	execution.probeErr = errors.New("native API unavailable")
	engine := boundStarter(execution)
	service := newService(t, engine)
	if _, err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err := service.Start(context.Background())
	if !errors.Is(err, execution.probeErr) || status.Phase != instance.PhaseFailed || status.Process.Exit != nil || engine.calls.Load() != 1 || status.AppliedRevision != engine.revision {
		t.Fatalf("failed API probe was confused with process exit or restart: %+v, %v", status, err)
	}
	if _, err := service.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestExitDuringReuseProbePreservesNativeCause(t *testing.T) {
	execution := newSession()
	nativeFailure := errors.New("native exit 42")
	apiFailure := errors.New("API connection closed")
	execution.probe = func(context.Context) error {
		execution.mu.Lock()
		defer execution.mu.Unlock()
		execution.observation.Exit = &instance.Exit{At: time.Now(), Code: 42, Cause: nativeFailure}
		execution.observation.Failure = nativeFailure
		execution.observation.CleanupComplete = true
		return apiFailure
	}
	service := newService(t, boundStarter(execution))
	if _, err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err := service.Start(context.Background())
	if !errors.Is(err, nativeFailure) || !errors.Is(err, instance.ErrUnexpectedExit) || !errors.Is(err, apiFailure) || status.Phase != instance.PhaseFailed {
		t.Fatalf("reuse probe hid the confirmed process exit: %+v, %v", status, err)
	}
}

func TestCancelledStartCannotCommitReturnedSession(t *testing.T) {
	execution := newSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine := boundStarter(execution)
	engine.start = func(context.Context) (instance.ISession, error) { cancel(); return execution, nil }
	service := newService(t, engine)
	status, err := service.Start(ctx)
	if !errors.Is(err, context.Canceled) || status.AppliedRevision != "" || execution.stops.Load() != 1 || !status.Process.CleanupComplete {
		t.Fatalf("cancelled startup leaked or committed: %+v, %v", status, err)
	}
}

func TestStopInterruptsObservableStartup(t *testing.T) {
	entered := make(chan struct{})
	engine := boundStarter(nil)
	engine.start = func(ctx context.Context) (instance.ISession, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	service := newService(t, engine)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := service.Start(ctx); done <- err }()
	joined := false
	defer func() {
		cancel()
		if !joined {
			<-done
		}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("startup did not enter engine")
	}
	if status := service.Status(); status.Phase != instance.PhaseStarting || status.AppliedRevision != "" {
		t.Fatalf("starting state unavailable: %+v", status)
	}
	if _, err := service.Start(ctx); !errors.Is(err, instance.ErrBusy) {
		t.Fatal("conflicting start admitted")
	}
	status, err := service.Stop(ctx)
	if err != nil || status.Phase != instance.PhaseStopped {
		t.Fatalf("startup stop failed: %+v, %v", status, err)
	}
	err = <-done
	joined = true
	if !errors.Is(err, context.Canceled) {
		t.Fatal("startup interruption cause lost")
	}
}

func TestStopOwnsCleanupAfterCallerStopsWaiting(t *testing.T) {
	execution := newSession()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	releaseStop := func() { once.Do(func() { close(release) }) }
	defer releaseStop()
	execution.stop = func(ctx context.Context) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	service := newService(t, boundStarter(execution))
	if _, err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		releaseStop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, err := service.Stop(ctx); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := service.Stop(ctx); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("owned stop did not start")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("caller cancellation lost")
	}
	if status := service.Status(); status.Phase != instance.PhaseStopping {
		t.Fatalf("prematurely stopped: %+v", status)
	}
	releaseStop()
	joinCtx, joinCancel := context.WithTimeout(context.Background(), time.Second)
	defer joinCancel()
	status, err := service.Stop(joinCtx)
	if err != nil || status.Phase != instance.PhaseStopped || execution.stops.Load() != 1 {
		t.Fatalf("cleanup ownership lost: %+v, %v", status, err)
	}
}

func BenchmarkStatus(b *testing.B) {
	service := newService(b, boundStarter(newSession()))
	if _, err := service.Start(context.Background()); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		service.Status()
	}
}
