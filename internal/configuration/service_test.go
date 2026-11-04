package configuration_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/configuration"
	"github.com/papilio-platform/Node/internal/instance"
)

type candidate struct {
	source                        string
	checks, starts, probes, stops int
	checkErr, startErr            error
	probeErr                      error
	check                         func(context.Context) error
}
type execution struct {
	mu    sync.Mutex
	owner *candidate
	state instance.ProcessObservation
}

func (c *candidate) Revision() instance.Revision { return instance.Revision(c.source) }
func (c *candidate) Source() []byte              { return []byte(c.source) }
func (c *candidate) Validate(ctx context.Context) error {
	c.checks++
	if c.check != nil {
		return c.check(ctx)
	}
	return c.checkErr
}
func (c *candidate) Start(ctx context.Context) (instance.ISession, error) {
	c.starts++
	if c.startErr != nil {
		return nil, c.startErr
	}
	e := &execution{owner: c, state: instance.ProcessObservation{PID: c.starts, ReadyAt: time.Now()}}
	return e, nil
}
func (e *execution) PID() int { return e.state.PID }
func (e *execution) Observe() instance.ProcessObservation {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}
func (e *execution) Probe(context.Context) error { e.owner.probes++; return e.owner.probeErr }
func (e *execution) Stop(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.owner.stops++
	e.state.Exit = &instance.Exit{Code: 0}
	e.state.CleanupComplete = true
	return nil
}
func (e *execution) Wait(context.Context) error { return nil }

type factory struct {
	next  *candidate
	calls int
}

func (f *factory) Capture(source []byte) (configuration.ICandidate, error) {
	f.calls++
	if len(source) > 32 {
		return nil, errors.New("too large")
	}
	f.next.source = string(source)
	return f.next, nil
}

func fixture(t *testing.T) (*configuration.Service, *instance.Service, *candidate, *factory) {
	t.Helper()
	old := &candidate{source: "old"}
	life, err := instance.New("primary", "controlled", old)
	if err != nil {
		t.Fatal(err)
	}
	f := &factory{next: &candidate{}}
	service, err := configuration.New(life, f, old)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { life.Stop(context.Background()) })
	return service, life, old, f
}

func TestValidateAndReadDoNotMutateExecution(t *testing.T) {
	s, life, old, f := fixture(t)
	life.Start(t.Context())
	v, err := s.Validate(t.Context(), []byte("new"))
	if err != nil || v.Revision != "new" || v.Against != "old" || !v.RestartRequired || f.next.checks != 1 {
		t.Fatalf("validation: %+v %v", v, err)
	}
	view, err := s.Read()
	if err != nil || !bytes.Equal(view.Source, []byte("old")) || view.State.AppliedRevision != "old" || old.stops != 0 || f.next.starts != 0 {
		t.Fatalf("validation changed state: %+v %v", view, err)
	}
	view.Source[0] = 'x'
	again, _ := s.Read()
	if string(again.Source) != "old" {
		t.Fatal("mutable source escaped")
	}
}

func TestInvalidAndStaleApplyLeaveHealthyExecution(t *testing.T) {
	s, life, old, f := fixture(t)
	life.Start(t.Context())
	if _, err := s.Apply(t.Context(), "stale", []byte("new")); !errors.Is(err, instance.ErrRevisionConflict) || f.calls != 0 {
		t.Fatal("stale apply executed work", err)
	}
	rejected := errors.New("native rejection")
	f.next.checkErr = rejected
	result, err := s.Apply(t.Context(), "old", []byte("new"))
	if !errors.Is(err, rejected) || result.Current.Phase != instance.PhaseReady || old.stops != 0 || f.next.starts != 0 {
		t.Fatalf("invalid source disrupted healthy run: %+v %v", result, err)
	}
}

func TestApplyReplacementAndRepeatedSourceReuse(t *testing.T) {
	s, life, old, f := fixture(t)
	life.Start(t.Context())
	result, err := s.Apply(t.Context(), "old", []byte("new"))
	if err != nil || result.Reused || result.Current.DesiredRevision != "new" || result.Current.AppliedRevision != "new" || old.stops != 1 || f.next.starts != 1 {
		t.Fatalf("replacement: %+v %v", result, err)
	}
	next := f.next
	f.next = &candidate{}
	result, err = s.Apply(t.Context(), "new", []byte("new"))
	if err != nil || !result.Reused || next.stops != 0 || next.probes != 1 || f.next.starts != 0 || f.next.checks != 0 {
		t.Fatalf("unchanged apply added validation or restarted run: %+v %v", result, err)
	}
}

func TestApplyFailureKeepsCandidateDesiredAndOldApplied(t *testing.T) {
	s, life, old, f := fixture(t)
	life.Start(t.Context())
	rejected := errors.New("listener conflict")
	f.next.startErr = rejected
	result, err := s.Apply(t.Context(), "old", []byte("new"))
	view, readErr := s.Read()
	if !errors.Is(err, rejected) || readErr != nil || result.Current.Phase != instance.PhaseFailed || result.Current.AppliedRevision != "old" || result.Current.DesiredRevision != "new" || string(view.Source) != "new" || old.stops != 1 {
		t.Fatalf("failed replacement facts: %+v %+v %v %v", result, view, err, readErr)
	}
	f.next = &candidate{}
	result, err = s.Apply(t.Context(), "new", []byte("new"))
	if err != nil || result.Current.AppliedRevision != "new" || result.Current.Phase != instance.PhaseReady {
		t.Fatalf("explicit recovery failed: %+v %v", result, err)
	}
}

func TestConfigurationCancellationAndInputBound(t *testing.T) {
	s, life, old, f := fixture(t)
	life.Start(t.Context())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Apply(ctx, "old", []byte("new")); !errors.Is(err, context.Canceled) || f.calls != 0 {
		t.Fatal("pre-cancelled apply performed work", err)
	}
	if _, err := s.Apply(t.Context(), "old", make([]byte, 33)); err == nil || old.stops != 0 {
		t.Fatal("oversized source disrupted run", err)
	}
}

func TestConfigurationConstructorAndOutOfBandBinding(t *testing.T) {
	s, life, old, f := fixture(t)
	if f.calls != 0 || old.checks != 0 || old.starts != 0 {
		t.Fatal("constructor executed business work")
	}
	if _, err := configuration.New(life, f, &candidate{source: "other"}); !errors.Is(err, instance.ErrRevisionConflict) {
		t.Fatal("mismatched initial binding accepted", err)
	}
	r, err := life.BeginReplacement(t.Context(), "old")
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Activate(&candidate{source: "out-of-band"})
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(); !errors.Is(err, instance.ErrRevisionConflict) {
		t.Fatal("read fabricated matching source", err)
	}
}

func TestUnhealthyUnchangedApplyValidatesBeforeRecovery(t *testing.T) {
	s, life, old, f := fixture(t)
	life.Start(t.Context())
	old.probeErr = errors.New("native API unavailable")
	result, err := s.Apply(t.Context(), "old", []byte("old"))
	if err != nil || result.Reused || old.probes != 1 || old.stops != 1 || f.next.checks != 1 || f.next.starts != 1 || result.Current.Phase != instance.PhaseReady {
		t.Fatalf("unhealthy unchanged binding was not recovered: %+v %v", result, err)
	}
}
