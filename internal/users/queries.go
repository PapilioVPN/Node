package users

import (
	"context"
	"errors"
)

// Get reads one native user. Email is an engine identifier, not RFC email input.
func (s *Service) Get(ctx context.Context, target Target, email string) (User, error) {
	if len(email) == 0 || len(email) > MaxIdentifierBytes {
		return User{}, ErrInvalid
	}
	access, err := s.acquire(ctx, target)
	if err != nil {
		return User{}, err
	}
	defer access.Close()
	user, err := s.reader.Get(access.Context(), access.Session(), target.Inbound, email)
	if err = errors.Join(err, access.Context().Err()); err != nil {
		return User{}, err
	}
	return user, nil
}

// Count reads the native count without fetching the users.
func (s *Service) Count(ctx context.Context, target Target) (int64, error) {
	access, err := s.acquire(ctx, target)
	if err != nil {
		return 0, err
	}
	defer access.Close()
	count, err := s.reader.Count(access.Context(), access.Session(), target.Inbound)
	if err = errors.Join(err, access.Context().Err()); err != nil {
		return 0, err
	}
	return count, nil
}

// List requests a complete collection of at most limit users (1..maxUsers).
func (s *Service) List(ctx context.Context, target Target, limit int) ([]User, error) {
	if limit < 1 || limit > s.maxUsers {
		return nil, ErrInvalid
	}
	access, err := s.acquire(ctx, target)
	if err != nil {
		return nil, err
	}
	defer access.Close()
	result, err := s.reader.List(access.Context(), access.Session(), target.Inbound, limit)
	if err = errors.Join(err, access.Context().Err()); err != nil {
		return nil, err
	}
	return result, nil
}
