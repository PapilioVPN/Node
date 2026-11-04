// Package instance owns the lifecycle policy of a bound managed engine process.
package instance

import (
	"context"
	"errors"
	"time"
)

// Revision identifies captured configuration and its preparation policy.
type Revision string

// Phase describes application lifecycle state, not a fresh health check.
type Phase string

const (
	PhaseStopped  Phase = "stopped"
	PhaseStarting Phase = "starting"
	PhaseReady    Phase = "ready"
	PhaseStopping Phase = "stopping"
	PhaseFailed   Phase = "failed"
)

var (
	// ErrBusy indicates an active conflicting lifecycle operation.
	ErrBusy = errors.New("instance lifecycle operation in progress")
	// ErrFailed requires explicit stop/recovery before another start.
	ErrFailed = errors.New("instance requires explicit recovery")
	// ErrUnexpectedExit includes successful exits while the instance should run.
	ErrUnexpectedExit = errors.New("engine exited unexpectedly")
	// ErrCleanupIncomplete means owned resources could not be confirmed released.
	ErrCleanupIncomplete = errors.New("engine cleanup incomplete")
	// ErrRevisionConflict rejects a replacement based on different desired input.
	ErrRevisionConflict = errors.New("instance desired revision has changed")
	// ErrExecutionConflict rejects a target from a different execution generation.
	ErrExecutionConflict = errors.New("instance execution has changed")
	// ErrNotReady rejects native access without a recorded ready execution.
	ErrNotReady = errors.New("instance is not ready")
)

// Exit records confirmed process termination. Code is -1 for a signal exit.
type Exit struct {
	At     time.Time
	Code   int
	Signal int
	Cause  error
}

// ProcessObservation separates process exit, resource cleanup and API readiness.
// Exit is nil until termination is confirmed. Error values are shared read-only.
type ProcessObservation struct {
	PID               int
	Version           string
	VersionObservedAt time.Time
	StartedAt         time.Time
	ReadyAt           time.Time
	Exit              *Exit
	// CleanupComplete marks a finished attempt; CleanupFailure determines success.
	CleanupComplete bool
	CleanupFailure  error
	Failure         error
}

// Snapshot reports the current operation and observed execution facts.
// AppliedRevision and ReadyAt are historical; ObservedAt is not API freshness.
type Snapshot struct {
	// Generation is local to this Service and changes for every startup attempt.
	Generation      uint64
	ID              string
	Engine          string
	Phase           Phase
	DesiredRevision Revision
	AppliedRevision Revision
	Process         ProcessObservation
	ObservedAt      time.Time
	LastFailure     error
}

// RestartResult separates the previous execution's stop outcome from replacement
// readiness. StopFailure may be non-nil after a successful recovery. Current is a
// snapshot, not a durable operation receipt; errors remain shared read-only.
type RestartResult struct {
	Previous    Snapshot
	Current     Snapshot
	StopFailure error
}

// ISession owns one managed engine process and supports concurrent calls. Observe
// performs no I/O. Probe honors cancellation and adapter limits, including while
// Stop runs. Stop owns bounded cleanup even if ctx expires;
// Stop and Wait return only after cleanup unless their waiting context ends.
type ISession interface {
	PID() int
	Observe() ProcessObservation
	Probe(context.Context) error
	Stop(context.Context) error
	Wait(context.Context) error
}

// IStarter binds immutable input. Revision performs no I/O; a failed Start joins
// owned cleanup before returning, or reports ErrCleanupIncomplete. Returned
// sessions outlive the start request.
type IStarter interface {
	Revision() Revision
	Start(context.Context) (ISession, error)
}
