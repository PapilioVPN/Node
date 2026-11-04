package instance

import (
	"context"
	"errors"
	"sync"
)

// Access pins one ready execution for a native capability operation. Its owner
// must Close after joining its work. Service.Stop cancels Context and joins Close.
type Access struct {
	service *Service
	context context.Context
	cancel  context.CancelFunc
	session ISession
	done    chan struct{}
	once    sync.Once
}

// Acquire rejects stale generation or contended lifecycle/capability access.
// It does no native work. Generation is local to this Service lifetime.
func (s *Service) Acquire(ctx context.Context, generation uint64) (*Access, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if generation != s.generation {
		return nil, ErrExecutionConflict
	}
	if s.access != nil || s.replacing != nil || s.starting != nil || s.stopping != nil || s.probing != nil {
		return nil, ErrBusy
	}
	status := s.snapshotLocked()
	if status.Phase != PhaseReady {
		return nil, errors.Join(ErrNotReady, status.LastFailure)
	}
	accessCtx, cancel := context.WithCancel(ctx)
	access := &Access{service: s, context: accessCtx, cancel: cancel, session: s.session, done: make(chan struct{})}
	s.access = access
	return access, nil
}

// Context is cancelled by the caller or Service.Stop.
func (a *Access) Context() context.Context { return a.context }

// Session is borrowed until Close; capability adapters must not stop or retain it.
func (a *Access) Session() ISession { return a.session }

// Close releases admission and is safe to call repeatedly or concurrently.
func (a *Access) Close() {
	a.once.Do(func() {
		a.cancel()
		a.service.mu.Lock()
		a.service.access = nil
		close(a.done)
		a.service.mu.Unlock()
	})
}
