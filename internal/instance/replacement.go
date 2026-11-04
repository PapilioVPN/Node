package instance

import (
	"context"
	"errors"
)

type replacementAttempt struct {
	cancel        context.CancelFunc
	done          chan struct{}
	stopRequested bool
	cleanupErr    error // Read by Stop only after done closes.
}

// Replacement reserves one lifecycle transition, including external preflight.
// Its single owner must call Close and join its result. Other callers may Stop
// the service concurrently; Stop cancels Context and waits for Close.
type Replacement struct {
	service     *Service
	attempt     *replacementAttempt
	context     context.Context
	cancel      context.CancelFunc
	initial     Snapshot
	activated   bool
	closed      bool
	closeStatus Snapshot
	closeErr    error
}

// BeginReplacement conditionally reserves the current desired content revision.
// A mismatch or conflicting operation performs no engine work.
func (s *Service) BeginReplacement(ctx context.Context, expected Revision) (*Replacement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.snapshotLocked()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if expected != s.revision {
		return nil, ErrRevisionConflict
	}
	return s.reserveReplacementLocked(ctx, status)
}

func (s *Service) reserveReplacementLocked(ctx context.Context, status Snapshot) (*Replacement, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.replacing != nil || s.starting != nil || s.stopping != nil || s.probing != nil {
		return nil, ErrBusy
	}
	operationCtx, cancel := context.WithCancel(ctx)
	attempt := &replacementAttempt{cancel: cancel, done: make(chan struct{})}
	s.replacing = attempt
	return &Replacement{service: s, attempt: attempt, context: operationCtx, cancel: cancel, initial: status}, nil
}

// Context includes caller cancellation and interruption by Service.Stop.
func (r *Replacement) Context() context.Context { return r.context }

// Activate binds an immutable starter after previous cleanup, then starts it.
// Failed startup keeps the new desired binding and historical applied revision.
func (r *Replacement) Activate(starter IStarter) (RestartResult, error) {
	s := r.service
	result := RestartResult{Previous: r.initial, Current: s.Status()}
	if r.closed || r.activated {
		return result, ErrBusy
	}
	if starter == nil || starter.Revision() == "" {
		return result, errors.New("replacement starter and revision are required")
	}
	revision := starter.Revision()
	if err := r.context.Err(); err != nil {
		return result, err
	}
	r.activated = true
	result.Previous, result.StopFailure = s.stop(context.Background(), r.attempt)
	r.attempt.cleanupErr = result.StopFailure
	if result.Previous.Phase != PhaseStopped {
		result.Current = s.Status()
		return result, errors.Join(ErrCleanupIncomplete, result.StopFailure, context.Cause(r.context))
	}
	s.mu.Lock()
	if err := r.context.Err(); err != nil {
		result.Current = s.snapshotLocked()
		s.mu.Unlock()
		return result, err
	}
	s.starter, s.revision = starter, revision
	s.mu.Unlock()
	current, err := s.start(r.context, r.attempt)
	result.Current = current
	return result, err
}

// Reuse probes the reserved ready execution without replacing it. Failed probes
// may be followed by Activate for explicit recovery within the same reservation.
func (r *Replacement) Reuse() (Snapshot, error) {
	if r.closed || r.activated {
		return r.service.Status(), ErrBusy
	}
	if err := r.context.Err(); err != nil {
		return r.service.Status(), err
	}
	status := r.service.Status()
	if status.Phase != PhaseReady {
		return status, errors.Join(ErrFailed, status.LastFailure)
	}
	return r.service.start(r.context, r.attempt)
}

// Close releases the reservation, joining any cleanup requested by Stop.
// It must run even if external preflight fails. It is idempotent.
func (r *Replacement) Close() (Snapshot, error) {
	if r.closed {
		return r.closeStatus, r.closeErr
	}
	s := r.service
	s.mu.Lock()
	if r.attempt.stopRequested {
		s.mu.Unlock()
		stopped, cleanupErr := s.stop(context.Background(), r.attempt)
		s.mu.Lock()
		r.attempt.cleanupErr = errors.Join(r.attempt.cleanupErr, cleanupErr, stopped.Process.Failure)
		r.closeErr = errors.Join(context.Canceled, cleanupErr)
	}
	r.closeStatus = s.snapshotLocked()
	s.replacing = nil
	close(r.attempt.done)
	s.mu.Unlock()
	r.cancel()
	r.closed = true
	return r.closeStatus, r.closeErr
}
