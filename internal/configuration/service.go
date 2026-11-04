// Package configuration owns sensitive engine-source validation and conditional
// full replacement, independently of engine syntax and native execution.
package configuration

import (
	"context"
	"errors"
	"sync"

	"github.com/papilio-platform/Node/internal/instance"
)

// ICandidate owns immutable captured input. Revision and Source perform no I/O;
// Source returns an independent copy. Validate checks effective execution input
// without starting engine listeners.
type ICandidate interface {
	instance.IStarter
	Source() []byte
	Validate(context.Context) error
}

// IFactory captures bounded source without I/O into a fresh immutable candidate.
type IFactory interface {
	Capture([]byte) (ICandidate, error)
}

// ILifecycle is the existing instance's scoped replacement capability.
type ILifecycle interface {
	Status() instance.Snapshot
	BeginReplacement(context.Context, instance.Revision) (*instance.Replacement, error)
}

// View combines sensitive original desired bytes with lifecycle observations.
// AppliedRevision is historical; the source is not a runtime-engine dump.
type View struct {
	Source []byte
	State  instance.Snapshot
}

// Validation describes native acceptance against an observed desired revision.
// RestartRequired describes full replacement, not a live-update plan or readiness.
type Validation struct {
	Revision        instance.Revision
	Against         instance.Revision
	RestartRequired bool
}

// ApplyResult preserves old cleanup and actual current state separately. Reused
// is true only when the same ready execution passed an actual native probe.
type ApplyResult struct {
	instance.RestartResult
	Reused bool
}

// Service owns one configuration binding and admits one validation/apply at a
// time. Composition must create exactly one service per lifecycle owner and use
// it for all configuration changes. Read reports ErrBusy during these operations.
type Service struct {
	lifecycle ILifecycle
	factory   IFactory
	// Lock order: mu -> lifecycle observation locks. Native work runs unlocked.
	mu      sync.Mutex
	current ICandidate
	active  bool
}

// New binds the initial captured candidate without engine I/O.
func New(lifecycle ILifecycle, factory IFactory, initial ICandidate) (*Service, error) {
	if lifecycle == nil || factory == nil || initial == nil || initial.Revision() == "" {
		return nil, errors.New("configuration lifecycle, factory and initial candidate are required")
	}
	if lifecycle.Status().DesiredRevision != initial.Revision() {
		return nil, instance.ErrRevisionConflict
	}
	return &Service{lifecycle: lifecycle, factory: factory, current: initial}, nil
}

// Read returns owned original bytes, without engine I/O. Other lifecycle users
// may change process facts; out-of-band rebindings return ErrRevisionConflict.
func (s *Service) Read() (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active {
		return View{}, instance.ErrBusy
	}
	state := s.lifecycle.Status()
	if state.DesiredRevision != s.current.Revision() {
		return View{}, instance.ErrRevisionConflict
	}
	return View{Source: s.current.Source(), State: state}, nil
}

func (s *Service) begin(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.active {
		return instance.ErrBusy
	}
	s.active = true
	return nil
}

func (s *Service) finish() {
	s.mu.Lock()
	s.active = false
	s.mu.Unlock()
}

func (s *Service) capture(source []byte) (ICandidate, error) {
	candidate, err := s.factory.Capture(source)
	if err != nil {
		return nil, err
	}
	if candidate == nil || candidate.Revision() == "" {
		return nil, errors.New("engine returned no captured configuration")
	}
	return candidate, nil
}
