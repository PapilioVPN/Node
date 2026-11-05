package xray

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"github.com/papilio-platform/Node/internal/enginelog"
	"github.com/papilio-platform/Node/internal/instance"
)

// Start checks configuration and returns after the private native API responds.
// Cancellation before success joins cleanup. After success l.lifetime owns the
// session, independently of ctx. Callers own admission of conflicting starts.
func (l *Launcher) Start(ctx context.Context) (session instance.ISession, err error) {
	ctx, cancel := context.WithCancelCause(ctx)
	stopLifetimeWatch := context.AfterFunc(l.lifetime, func() { cancel(context.Cause(l.lifetime)) })
	defer stopLifetimeWatch()
	defer cancel(nil)
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
	}
	if err := l.lifetime.Err(); err != nil {
		return nil, context.Cause(l.lifetime)
	}
	version, err := readVersion(ctx, l.options.Check)
	if err != nil {
		return nil, err
	}
	versionObservedAt := time.Now().UTC()
	prepared, err := prepareRuntimeConfiguration(ctx, l.config, l.options.RuntimeDir, l.checker)
	if err != nil {
		return nil, err
	}
	var run *Session
	var api *nativeAPI
	defer func() {
		if session != nil {
			return
		}
		if run != nil {
			run.cancel()
			<-run.done
			err = errors.Join(err, run.result)
		} else {
			var cleanupErr error
			if api != nil {
				cleanupErr = api.connection.Close()
			}
			cleanupErr = errors.Join(cleanupErr, prepared.files.close())
			if cleanupErr != nil {
				err = errors.Join(err, instance.ErrCleanupIncomplete, cleanupErr)
			}
		}
		if ctx.Err() != nil {
			err = errors.Join(context.Cause(ctx), err)
		}
	}()
	socket := prepared.socket
	if err := l.checker.check(ctx, prepared.input); err != nil {
		return nil, err
	}
	api, err = newNativeAPI(socket)
	if err != nil {
		return nil, fmt.Errorf("create Xray API client: %w", err)
	}
	processCtx, stopProcess := context.WithCancel(l.lifetime)
	cmd := exec.CommandContext(processCtx, l.options.Check.Executable, prepared.input.arguments(false)...)
	cmd.Dir = l.options.Check.WorkingDir
	cmd.Env = nativeEnvironment(l.options.Check.AssetsDir)
	cmd.Stdin = bytes.NewReader(prepared.input.stdin)
	output := &diagnosticTail{data: make([]byte, l.options.Check.MaxDiagnosticBytes)}
	cmd.Stdout, cmd.Stderr = output, output
	var runtimeLogs *runtimeOutput
	if l.options.Logs != nil {
		runtimeLogs = newRuntimeOutput(output, l.options.Logs)
		cmd.Stdout, cmd.Stderr = runtimeLogs.stdout, runtimeLogs.stderr
	}
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = l.options.StopTimeout
	if err := ctx.Err(); err != nil {
		stopProcess()
		return nil, context.Cause(ctx)
	}
	// Gate output copying until the actual child identity is available.
	if runtimeLogs != nil {
		runtimeLogs.mu.Lock()
	}
	startErr := cmd.Start()
	startedAt := time.Now().UTC()
	if runtimeLogs != nil {
		if startErr == nil {
			runtimeLogs.execution = enginelog.Execution{PID: cmd.Process.Pid, StartedAt: startedAt}
		}
		runtimeLogs.mu.Unlock()
	}
	if startErr != nil {
		stopProcess()
		return nil, fmt.Errorf("start Xray process: %w", startErr)
	}
	run = &Session{cmd: cmd, api: api, files: prepared.files, output: output,
		runtimeLogs: runtimeLogs,
		context:     processCtx, cancel: stopProcess, done: make(chan struct{}),
		probeTimeout: l.options.ReadyTimeout,
		observation: instance.ProcessObservation{PID: cmd.Process.Pid, Version: version,
			VersionObservedAt: versionObservedAt, StartedAt: startedAt}}
	go run.reap()
	readyCtx, cancelReady := context.WithTimeout(ctx, l.options.ReadyTimeout)
	defer cancelReady()
	if err := run.Probe(readyCtx); err != nil {
		return nil, fmt.Errorf("wait for Xray API readiness: %w", errors.Join(context.Cause(readyCtx), err))
	}
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
	}
	if err := l.lifetime.Err(); err != nil {
		return nil, context.Cause(l.lifetime)
	}
	select {
	case <-run.done:
		return nil, ErrSessionExited
	default:
	}
	return run, nil
}
