package xray

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// ErrSessionExited indicates that a completed session cannot be probed.
var ErrSessionExited = errors.New("Xray session has exited")

// Session owns one engine, native client and completion wait. Its operations are
// concurrency-safe. The caller must Stop or cancel its lifetime and join Wait.
type Session struct {
	cmd       *exec.Cmd
	api       *nativeAPI
	directory string
	output    *diagnosticTail
	context   context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	result    error // Read only after done closes.
}

// Probe calls the owned engine's native API.
func (s *Session) Probe(ctx context.Context) error {
	select {
	case <-s.done:
		return errors.Join(ErrSessionExited, s.result)
	default:
	}
	return s.api.probe(ctx)
}

// PID returns the owned child process identifier.
func (s *Session) PID() int { return s.cmd.Process.Pid }

// Stop requests SIGTERM with bounded escalation and waits for cleanup. ctx only
// bounds the caller's wait; cancellation does not abandon shutdown. Repeated and
// concurrent calls join the same cleanup. Forced termination remains an error.
func (s *Session) Stop(ctx context.Context) error {
	s.cancel()
	return s.Wait(ctx)
}

// Wait waits for process exit and cleanup without initiating shutdown.
func (s *Session) Wait(ctx context.Context) error {
	select {
	case <-s.done:
		return s.result
	default:
	}
	select {
	case <-s.done:
		return s.result
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Session) reap() {
	err := s.cmd.Wait()
	if err == s.context.Err() {
		// A graceful exit after CommandContext cancellation reports its context.
		err = nil
	}
	err = errors.Join(err, s.api.connection.Close(), os.RemoveAll(s.directory))
	if err != nil {
		s.result = &ProcessError{cause: err, Diagnostics: s.output.bytes(), Truncated: s.output.truncated}
	}
	s.output.data = nil
	s.cancel()
	close(s.done)
}

// ProcessError preserves native exit/cleanup causes with private bounded output.
// The completed error and its diagnostic bytes are shared read-only by waiters.
type ProcessError struct {
	Diagnostics []byte
	Truncated   bool
	cause       error
}

func (e *ProcessError) Error() string { return fmt.Sprintf("Xray process: %v", e.cause) }

// Unwrap preserves the process exit and resource cleanup causes.
func (e *ProcessError) Unwrap() error { return e.cause }
