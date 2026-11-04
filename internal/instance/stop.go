package instance

import (
	"context"
	"errors"
)

// Stop owns accepted cleanup independently of the caller's waiting context.
// Repeated callers join the current operation; a cancelled caller can join later.
// Shutdown cancels and joins any admitted reuse probe before admitting a new run.
func (s *Service) Stop(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return s.Status(), err
	}
	s.mu.Lock()
	status := s.snapshotLocked()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return status, err
	}
	if s.starting != nil {
		attempt := s.starting
		attempt.stopRequested = true
		s.phase = PhaseStopping
		s.mu.Unlock()
		attempt.cancel()
		select {
		case <-attempt.done:
			return s.Status(), attempt.cleanupErr
		case <-ctx.Done():
			return s.Status(), ctx.Err()
		}
	}
	if status.Phase == PhaseStopped {
		s.mu.Unlock()
		return status, nil
	}
	if s.stopping != nil {
		operation := s.stopping
		s.mu.Unlock()
		return s.waitStop(ctx, operation)
	}
	if s.session == nil {
		if errors.Is(s.failure, ErrCleanupIncomplete) {
			s.mu.Unlock()
			return status, status.LastFailure
		}
		s.phase = PhaseStopped
		status = s.snapshotLocked()
		s.mu.Unlock()
		return status, nil
	}
	s.failure = status.LastFailure
	s.phase = PhaseStopping
	operation := &stopOperation{done: make(chan struct{})}
	s.stopping = operation
	session := s.session
	probe := s.probing
	s.mu.Unlock()
	go s.finishStop(session, probe, operation)
	return s.waitStop(ctx, operation)
}

func (s *Service) finishStop(session ISession, probe *probeAttempt, operation *stopOperation) {
	if probe != nil {
		probe.cancel()
	}
	err := session.Stop(context.Background())
	if probe != nil {
		<-probe.done
	}
	observation := session.Observe()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !observation.CleanupComplete || observation.CleanupFailure != nil {
		err = errors.Join(ErrCleanupIncomplete, err, observation.CleanupFailure)
		s.phase = PhaseFailed
	} else {
		s.phase = PhaseStopped
	}
	if err != nil {
		s.failure = err
	}
	operation.err = err
	s.stopping = nil
	close(operation.done)
}

func (s *Service) waitStop(ctx context.Context, operation *stopOperation) (Snapshot, error) {
	select {
	case <-operation.done:
		return s.Status(), operation.err
	case <-ctx.Done():
		return s.Status(), ctx.Err()
	}
}
