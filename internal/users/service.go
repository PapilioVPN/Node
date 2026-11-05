// Package users owns execution-scoped user queries and conditional mutations.
package users

import (
	"context"
	"errors"

	"github.com/papilio-platform/Node/internal/instance"
)

// MaxListUsers bounds one complete metadata collection.
const MaxListUsers = 100000

// MaxIdentifierBytes bounds inbound and lookup identifiers in bytes.
const MaxIdentifierBytes = 256

var (
	// ErrInvalid rejects invalid user inputs and resource bounds.
	ErrInvalid = errors.New("invalid user input")
	// ErrNotFound means the requested user does not exist on the selected inbound.
	ErrNotFound = errors.New("user not found")
	// ErrLimit means a complete native result exceeds the configured item/byte cap.
	ErrLimit = errors.New("user observation exceeds its limit")
	// ErrUnsupported means the execution lacks the requested user capability.
	ErrUnsupported = errors.New("user operation is unsupported")
)

// Target identifies one inbound on a Service-local execution generation.
type Target struct {
	Generation uint64
	Inbound    string
}

// AccountRevision fingerprints observed native account content within a session.
type AccountRevision string

// User exposes identity/level metadata and a conditional mutation token, never credentials.
type User struct {
	Email    string
	Level    uint32
	Revision AccountRevision
}

// IAccess pins a ready execution and joins Stop through the instance owner.
type IAccess interface {
	Acquire(context.Context, uint64) (*instance.Access, error)
}

// IReader borrows a pinned session until return, honors cancellation and native
// limits, and returns no generated messages. List returns the complete bounded
// collection or an error, never a silently truncated result or fake page.
type IReader interface {
	Get(context.Context, instance.ISession, string, string) (User, error)
	Count(context.Context, instance.ISession, string) (int64, error)
	List(context.Context, instance.ISession, string, int) ([]User, error)
}

// Service coordinates one binding's queries without storing a second user index.
type Service struct {
	access   IAccess
	reader   IReader
	maxUsers int
}

// New requires an explicit per-query collection bound of 1..100000 users.
func New(access IAccess, reader IReader, maxUsers int) (*Service, error) {
	if access == nil || reader == nil || maxUsers < 1 || maxUsers > MaxListUsers {
		return nil, ErrInvalid
	}
	return &Service{access: access, reader: reader, maxUsers: maxUsers}, nil
}

func valid(target Target) bool {
	return target.Generation != 0 && len(target.Inbound) > 0 && len(target.Inbound) <= MaxIdentifierBytes
}

func (s *Service) acquire(ctx context.Context, target Target) (*instance.Access, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !valid(target) {
		return nil, ErrInvalid
	}
	return s.access.Acquire(ctx, target.Generation)
}
