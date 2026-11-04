package users_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/papilio-platform/Node/internal/instance"
	"github.com/papilio-platform/Node/internal/users"
)

type session struct {
	mu      sync.Mutex
	stopped bool
}

func (s *session) PID() int                    { return 42 }
func (s *session) Probe(context.Context) error { return nil }
func (s *session) Stop(context.Context) error {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	return nil
}
func (s *session) Wait(context.Context) error { return nil }
func (s *session) Observe() instance.ProcessObservation {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := instance.ProcessObservation{PID: 42}
	if s.stopped {
		o.Exit = &instance.Exit{}
		o.CleanupComplete = true
	}
	return o
}

type starter struct{ session *session }

func (s starter) Revision() instance.Revision                      { return "revision" }
func (s starter) Start(context.Context) (instance.ISession, error) { return s.session, nil }

type reader struct {
	gets, counts, lists int
	err                 error
	blocking            bool
}

func (r *reader) Get(ctx context.Context, _ instance.ISession, inbound, email string) (users.User, error) {
	r.gets++
	if r.blocking {
		<-ctx.Done()
		return users.User{}, ctx.Err()
	}
	return users.User{Email: email, Level: 7}, r.err
}
func (r *reader) Count(context.Context, instance.ISession, string) (int64, error) {
	r.counts++
	return 3, r.err
}
func (r *reader) List(context.Context, instance.ISession, string, int) ([]users.User, error) {
	r.lists++
	return []users.User{{Email: "first"}, {Email: "second"}}, r.err
}
func fixture(t *testing.T) (*users.Service, *instance.Service, *reader, users.Target) {
	t.Helper()
	life, err := instance.New("primary", "controlled", starter{&session{}})
	if err != nil {
		t.Fatal(err)
	}
	status, err := life.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	r := &reader{}
	s, err := users.New(life, r, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { life.Stop(context.Background()) })
	return s, life, r, users.Target{Generation: status.Generation, Inbound: "owned"}
}

func TestFocusedQueriesAndErrorOwnership(t *testing.T) {
	s, life, r, target := fixture(t)
	u, err := s.Get(t.Context(), target, "account")
	if err != nil || u.Email != "account" || u.Level != 7 || r.gets != 1 || r.lists != 0 || r.counts != 0 {
		t.Fatalf("single path: %+v %v", u, err)
	}
	n, err := s.Count(t.Context(), target)
	if err != nil || n != 3 || r.counts != 1 || r.lists != 0 {
		t.Fatal("count fetched users", err)
	}
	list, err := s.List(t.Context(), target, 2)
	if err != nil || len(list) != 2 || r.lists != 1 {
		t.Fatal("collection query", err)
	}
	r.err = users.ErrNotFound
	if _, err := s.Get(t.Context(), target, "missing"); !errors.Is(err, users.ErrNotFound) {
		t.Fatal("not-found hidden", err)
	}
	if life.Status().Phase != instance.PhaseReady {
		t.Fatal("query error changed lifecycle")
	}
	r.err = nil
	if _, err := s.Count(t.Context(), target); err != nil {
		t.Fatal("query admission leaked", err)
	}
}

func TestQueryBoundsCancellationAndStaleExecution(t *testing.T) {
	s, _, r, target := fixture(t)
	if _, err := s.Get(t.Context(), target, ""); !errors.Is(err, users.ErrInvalid) {
		t.Fatal("empty identifier became fetch-all", err)
	}
	if _, err := s.List(t.Context(), target, 3); !errors.Is(err, users.ErrInvalid) {
		t.Fatal("unbounded list", err)
	}
	wrong := target
	wrong.Generation++
	if _, err := s.Get(t.Context(), wrong, "account"); !errors.Is(err, instance.ErrExecutionConflict) {
		t.Fatal("stale execution", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Count(ctx, target); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled query", err)
	}
	if r.gets+r.counts+r.lists != 0 {
		t.Fatal("rejected query executed native work")
	}
}

func TestStopCancelsQueryAndJoinsItsScope(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, life, r, target := fixture(t)
		r.blocking = true
		done := make(chan error, 1)
		go func() { _, err := s.Get(t.Context(), target, "account"); done <- err }()
		synctest.Wait()
		if _, err := s.Count(t.Context(), target); !errors.Is(err, instance.ErrBusy) {
			t.Fatal("unbounded concurrent query", err)
		}
		stopped, err := life.Stop(t.Context())
		if err != nil || stopped.Phase != instance.PhaseStopped {
			t.Fatalf("stop did not join query: %+v %v", stopped, err)
		}
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal("query fabricated success", err)
		}
	})
}
