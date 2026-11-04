package instance

import (
	"context"
	"errors"
)

// Restart replaces the bound execution after its owned cleanup completes.
// Cancellation before replacement commit joins cleanup. Stop interrupts and joins
// the entire transition; conflicting Start/Restart calls return ErrBusy.
func (s *Service) Restart(ctx context.Context) (RestartResult, error) {
	s.mu.Lock()
	status := s.snapshotLocked()
	result := RestartResult{Previous: status, Current: status}
	replacement, err := s.reserveReplacementLocked(ctx, status)
	starter := s.starter
	s.mu.Unlock()
	if err != nil {
		return result, err
	}
	result, err = replacement.Activate(starter)
	current, closeErr := replacement.Close()
	result.Current = current
	return result, errors.Join(err, closeErr)
}
