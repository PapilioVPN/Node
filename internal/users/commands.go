package users

import (
	"context"
	"errors"
)

// Create adds an absent account or leaves an equal account unchanged.
func (c *Commands) Create(ctx context.Context, target Target, account Account) (MutationResult, error) {
	result := MutationResult{Outcome: OutcomeUnchanged}
	if !valid(target) || !validAccount(account) {
		return result, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	access, err := c.access.Acquire(ctx, target.Generation)
	if err != nil {
		return result, err
	}
	defer access.Close()
	ctx = access.Context()
	check, err := c.inspector.Check(ctx, access.Session(), target.Inbound, account)
	if err = errors.Join(err, ctx.Err()); err != nil {
		return result, err
	}
	if check.Exists {
		if check.Equal {
			result.Revision = check.Revision
			return result, nil
		}
		return result, ErrConflict
	}
	if check.AtCapacity {
		return result, ErrLimit
	}
	if err := c.writer.Add(ctx, access.Session(), target.Inbound, account); err != nil {
		result.Outcome = OutcomeUnknown
		return result, errors.Join(err, ctx.Err())
	}
	result.Outcome = OutcomeApplied
	result.Revision = check.DesiredRevision
	return result, ctx.Err()
}

// Replace requires the observed revision before removing changed input. Exact
// desired replay is a no-op even after the earlier revision has been replaced.
func (c *Commands) Replace(ctx context.Context, target Target, expected AccountRevision, account Account) (MutationResult, error) {
	result := MutationResult{Outcome: OutcomeUnchanged}
	if !valid(target) || !validAccount(account) || expected == "" || len(expected) > 128 {
		return result, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	access, err := c.access.Acquire(ctx, target.Generation)
	if err != nil {
		return result, err
	}
	defer access.Close()
	ctx = access.Context()
	check, err := c.inspector.Check(ctx, access.Session(), target.Inbound, account)
	if err = errors.Join(err, ctx.Err()); err != nil {
		return result, err
	}
	if !check.Exists {
		return result, ErrNotFound
	}
	if check.Equal {
		result.Revision = check.Revision
		return result, nil
	}
	if check.Revision != expected {
		return result, ErrConflict
	}
	if err := c.writer.Remove(ctx, access.Session(), target.Inbound, account.Email); err != nil {
		result.Outcome = OutcomeUnknown
		return result, errors.Join(err, ctx.Err())
	}
	result.PreviousRemoved = true
	result.Outcome = OutcomeAbsent
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := c.writer.Add(ctx, access.Session(), target.Inbound, account); err != nil {
		result.Outcome = OutcomeUnknown
		return result, errors.Join(err, ctx.Err())
	}
	result.Outcome = OutcomeApplied
	result.Revision = check.DesiredRevision
	return result, ctx.Err()
}

// Remove requires the observed revision and is idempotent for an absent identity.
// Established native streams may outlive authentication removal; this operation
// does not terminate them.
func (c *Commands) Remove(ctx context.Context, target Target, email string, expected AccountRevision) (MutationResult, error) {
	result := MutationResult{Outcome: OutcomeUnchanged}
	if !valid(target) || len(email) == 0 || len(email) > MaxIdentifierBytes || expected == "" || len(expected) > 128 {
		return result, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	access, err := c.access.Acquire(ctx, target.Generation)
	if err != nil {
		return result, err
	}
	defer access.Close()
	ctx = access.Context()
	check, err := c.inspector.CheckRemoval(ctx, access.Session(), target.Inbound, email)
	if err = errors.Join(err, ctx.Err()); err != nil {
		return result, err
	}
	if !check.Exists {
		result.Outcome = OutcomeAbsent
		return result, nil
	}
	if check.Revision != expected {
		return result, ErrConflict
	}
	if err := c.writer.Remove(ctx, access.Session(), target.Inbound, email); err != nil {
		result.Outcome = OutcomeUnknown
		return result, errors.Join(err, ctx.Err())
	}
	result.PreviousRemoved = true
	result.Outcome = OutcomeAbsent
	return result, ctx.Err()
}
