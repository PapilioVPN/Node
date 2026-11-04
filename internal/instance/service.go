package instance

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Service coordinates one managed-process binding and supports concurrent
// operations. Its owner must join Stop before releasing the service.
type Service struct {
	id       string
	engine   string
	starter  IStarter
	revision Revision
	// Lock order: Service.mu, then the session's observation lock; no I/O here.
	mu         sync.Mutex
	phase      Phase
	applied    Revision
	session    ISession
	failure    error
	generation uint64
	starting   *startAttempt
	stopping   *stopOperation
}

type startAttempt struct {
	cancel        context.CancelFunc
	done          chan struct{}
	stopRequested bool
	cleanupErr    error
}

type stopOperation struct {
	done chan struct{}
	err  error
}

// New constructs a stopped instance without executing engine work.
func New(id, engine string, starter IStarter) (*Service, error) {
	if id == "" || engine == "" || starter == nil {
		return nil, errors.New("instance identity, engine and starter are required")
	}
	revision := starter.Revision()
	if revision == "" {
		return nil, errors.New("instance configuration revision is required")
	}
	return &Service{id: id, engine: engine, starter: starter, revision: revision, phase: PhaseStopped}, nil
}

// Status reads process facts without engine I/O or refreshing API readiness.
func (s *Service) Status() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *Service) snapshotLocked() Snapshot {
	status := Snapshot{ID: s.id, Engine: s.engine, Phase: s.phase,
		DesiredRevision: s.revision, AppliedRevision: s.applied,
		ObservedAt: time.Now().UTC(), LastFailure: s.failure}
	if s.session != nil {
		status.Process = s.session.Observe()
		if s.phase == PhaseReady && status.Process.Exit != nil {
			status.Phase = PhaseFailed
			status.LastFailure = errors.Join(ErrUnexpectedExit, status.Process.Failure, status.Process.Exit.Cause)
		}
	}
	return status
}
