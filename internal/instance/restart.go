package instance

import (
	"context"
	"errors"
)

type restartAttempt struct {
	cancel        context.CancelFunc
	done          chan struct{}
	stopRequested bool
	cleanupErr    error // Read by Stop only after done closes.
}

// Restart replaces the bound execution after its owned cleanup completes.
// Cancellation before replacement commit joins cleanup. Stop interrupts and joins
// the entire transition; conflicting Start/Restart calls return ErrBusy.
func (s *Service) Restart(ctx context.Context) (RestartResult, error) {
	s.mu.Lock()
	status := s.snapshotLocked()
	result := RestartResult{Previous: status, Current: status}
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return result, err
	}
	if s.restarting != nil || s.starting != nil || s.stopping != nil || s.probing != nil {
		s.mu.Unlock()
		return result, ErrBusy
	}
	restartCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	attempt := &restartAttempt{cancel: cancel, done: make(chan struct{})}
	s.restarting = attempt
	s.mu.Unlock()

	result.Previous, result.StopFailure = s.stop(context.Background(), attempt)
	attempt.cleanupErr = result.StopFailure
	var err error
	if result.Previous.Phase != PhaseStopped {
		err = errors.Join(ErrCleanupIncomplete, result.StopFailure, context.Cause(restartCtx))
	} else {
		_, err = s.start(restartCtx, attempt)
	}

	s.mu.Lock()
	if attempt.stopRequested {
		s.mu.Unlock()
		stopped, cleanupErr := s.stop(context.Background(), attempt)
		s.mu.Lock()
		attempt.cleanupErr = errors.Join(attempt.cleanupErr, cleanupErr, stopped.Process.Failure)
		err = errors.Join(context.Canceled, err, cleanupErr)
	}
	result.Current = s.snapshotLocked()
	s.restarting = nil
	close(attempt.done)
	s.mu.Unlock()
	return result, err
}
