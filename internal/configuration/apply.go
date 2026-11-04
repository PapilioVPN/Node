package configuration

import (
	"context"
	"errors"

	"github.com/papilio-platform/Node/internal/instance"
)

// Apply validates before disrupting an engine, reserves expected desired input
// and joins cleanup on cancellation. It does not persist input or roll back a
// failed replacement. A later explicit call may recover the desired binding.
func (s *Service) Apply(ctx context.Context, expected instance.Revision, source []byte) (result ApplyResult, err error) {
	initial := s.lifecycle.Status()
	result.RestartResult = instance.RestartResult{Previous: initial, Current: initial}
	if err = s.begin(ctx); err != nil {
		return result, err
	}
	defer s.finish()
	replacement, err := s.lifecycle.BeginReplacement(ctx, expected)
	if err != nil {
		result.Current = s.lifecycle.Status()
		return result, err
	}
	var candidate ICandidate
	defer func() {
		current, closeErr := replacement.Close()
		result.Current = current
		err = errors.Join(err, closeErr)
		if !result.Reused && candidate != nil && current.DesiredRevision == candidate.Revision() {
			s.mu.Lock()
			s.current = candidate
			s.mu.Unlock()
		}
	}()
	candidate, err = s.capture(source)
	if err != nil {
		return result, err
	}
	state := s.lifecycle.Status()
	if candidate.Revision() == state.DesiredRevision && state.Phase == instance.PhaseReady {
		result.Current, err = replacement.Reuse()
		if err == nil {
			result.Reused = true
			return result, nil
		}
		if replacement.Context().Err() != nil {
			return result, err
		}
	}
	if err = candidate.Validate(replacement.Context()); err != nil {
		return result, errors.Join(err, replacement.Context().Err())
	}
	if err = replacement.Context().Err(); err != nil {
		return result, err
	}
	result.RestartResult, err = replacement.Activate(candidate)
	return result, err
}
