package instance

import (
	"context"
	"errors"
)

// Start starts the captured revision or probes/reuses its ready session. It never
// repairs a failed instance implicitly. Overlapping starts return ErrBusy, including
// while a reuse probe runs. Cancellation before commit joins cleanup.
func (s *Service) Start(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return s.Status(), err
	}
	s.mu.Lock()
	status := s.snapshotLocked()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return status, err
	}
	if s.probing != nil {
		s.mu.Unlock()
		return status, ErrBusy
	}
	switch status.Phase {
	case PhaseStarting, PhaseStopping:
		s.mu.Unlock()
		return status, ErrBusy
	case PhaseFailed:
		s.mu.Unlock()
		return status, errors.Join(ErrFailed, status.LastFailure)
	case PhaseReady:
		session, generation := s.session, s.generation
		probeCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		attempt := &probeAttempt{cancel: cancel, done: make(chan struct{})}
		s.probing = attempt
		s.mu.Unlock()
		err := session.Probe(probeCtx)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.probing = nil
		close(attempt.done)
		if probeCtx.Err() != nil {
			err = errors.Join(context.Cause(probeCtx), err)
		}
		if s.generation != generation || s.phase != PhaseReady {
			return s.snapshotLocked(), errors.Join(ErrBusy, err)
		}
		status = s.snapshotLocked()
		if status.Phase != PhaseReady {
			err = errors.Join(err, status.LastFailure)
		}
		if err != nil && ctx.Err() == nil {
			s.phase, s.failure = PhaseFailed, err
		}
		return s.snapshotLocked(), err
	}
	startCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	attempt := &startAttempt{cancel: cancel, done: make(chan struct{})}
	s.starting, s.session, s.failure = attempt, nil, nil
	s.phase = PhaseStarting
	s.generation++
	s.mu.Unlock()

	session, err := s.starter.Start(startCtx)
	s.mu.Lock()
	if startCtx.Err() != nil {
		err = errors.Join(context.Cause(startCtx), err)
	}
	if attempt.stopRequested {
		err = errors.Join(context.Canceled, err)
	}
	if session == nil && err == nil {
		err = errors.New("engine returned no session")
	}
	if session != nil {
		s.session = session
		observation := session.Observe()
		if observation.Exit != nil {
			err = errors.Join(ErrUnexpectedExit, err, observation.Failure, observation.Exit.Cause)
		}
	}
	if err != nil && session != nil {
		s.phase = PhaseStopping
		s.mu.Unlock()
		attempt.cleanupErr = session.Stop(context.Background())
		s.mu.Lock()
		err = errors.Join(err, attempt.cleanupErr)
	}
	if err == nil {
		s.phase, s.applied = PhaseReady, s.revision
	} else {
		s.phase, s.failure = PhaseFailed, err
		cleanupComplete := session == nil && !errors.Is(err, ErrCleanupIncomplete)
		if session != nil {
			observation := session.Observe()
			cleanupComplete = observation.CleanupComplete && observation.CleanupFailure == nil
		}
		if attempt.stopRequested && cleanupComplete {
			s.phase = PhaseStopped
		}
		if !cleanupComplete {
			attempt.cleanupErr = errors.Join(ErrCleanupIncomplete, err)
		}
	}
	s.starting = nil
	close(attempt.done)
	status = s.snapshotLocked()
	s.mu.Unlock()
	return status, err
}
