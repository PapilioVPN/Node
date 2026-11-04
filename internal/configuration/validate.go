package configuration

import (
	"context"
	"errors"

	"github.com/papilio-platform/Node/internal/instance"
)

// Validate borrows source until return and joins bounded native validation.
// It does not mutate either desired input or engine lifetime.
func (s *Service) Validate(ctx context.Context, source []byte) (Validation, error) {
	if err := s.begin(ctx); err != nil {
		return Validation{}, err
	}
	defer s.finish()
	candidate, err := s.capture(source)
	if err != nil {
		return Validation{}, err
	}
	if err := errors.Join(candidate.Validate(ctx), ctx.Err()); err != nil {
		return Validation{}, err
	}
	state := s.lifecycle.Status()
	return Validation{Revision: candidate.Revision(), Against: state.DesiredRevision,
		RestartRequired: candidate.Revision() != state.DesiredRevision || state.Phase != instance.PhaseReady}, nil
}
