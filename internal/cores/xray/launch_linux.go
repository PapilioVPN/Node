package xray

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/papilio-platform/Node/internal/instance"
)

// LaunchOptions supplies the native environment and owned runtime limits.
type LaunchOptions struct {
	Check        CheckerOptions
	RuntimeDir   string
	ReadyTimeout time.Duration
	StopTimeout  time.Duration
}

// Launcher binds one captured configuration to an explicit process lifetime.
type Launcher struct {
	lifetime context.Context
	options  LaunchOptions
	config   []byte
	checker  *Checker
	revision instance.Revision
}

// NewLauncher captures inputs without starting an engine.
func NewLauncher(lifetime context.Context, options LaunchOptions, config []byte) (*Launcher, error) {
	checker, err := NewChecker(options.Check)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(options.RuntimeDir) {
		return nil, errors.New("Xray runtime directory must be absolute")
	}
	if options.ReadyTimeout <= 0 || options.StopTimeout <= 0 {
		return nil, errors.New("Xray readiness and shutdown bounds must be positive")
	}
	if len(config) > options.Check.MaxConfigBytes {
		return nil, ErrConfigTooLarge
	}
	captured := bytes.Clone(config)
	return &Launcher{lifetime: lifetime, options: options, config: captured,
		checker: checker, revision: configurationRevision(captured)}, nil
}

// Revision identifies original bytes and the preparation policy, excluding the
// temporary socket path and externally referenced file contents.
func (l *Launcher) Revision() instance.Revision { return l.revision }

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
	directory, err := os.MkdirTemp(l.options.RuntimeDir, "xray-")
	if err != nil {
		return nil, fmt.Errorf("create Xray runtime directory: %w", err)
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
			cleanupErr = errors.Join(cleanupErr, os.RemoveAll(directory))
			if cleanupErr != nil {
				err = errors.Join(err, instance.ErrCleanupIncomplete, cleanupErr)
			}
		}
		if ctx.Err() != nil {
			err = errors.Join(context.Cause(ctx), err)
		}
	}()
	socket := filepath.Join(directory, "api.sock")
	if len(socket) > 107 {
		return nil, errors.New("Xray private socket path exceeds the Linux path limit")
	}
	overlay, err := prepareAPIOverlay(l.config, socket)
	if err != nil {
		return nil, fmt.Errorf("prepare Xray API configuration: %w", err)
	}
	path := filepath.Join(directory, "api.json")
	if err := os.WriteFile(path, overlay, 0600); err != nil {
		return nil, fmt.Errorf("write Xray API configuration: %w", err)
	}
	if err := l.checker.check(ctx, l.config, path); err != nil {
		return nil, err
	}
	api, err = newNativeAPI(socket)
	if err != nil {
		return nil, fmt.Errorf("create Xray API client: %w", err)
	}
	processCtx, stopProcess := context.WithCancel(l.lifetime)
	cmd := exec.CommandContext(processCtx, l.options.Check.Executable,
		"run", "-format=json", "-c", "stdin:", "-c", path)
	cmd.Dir = l.options.Check.WorkingDir
	cmd.Env = nativeEnvironment(l.options.Check.AssetsDir)
	cmd.Stdin = bytes.NewReader(l.config)
	output := &diagnosticTail{data: make([]byte, l.options.Check.MaxDiagnosticBytes)}
	cmd.Stdout, cmd.Stderr = output, output
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = l.options.StopTimeout
	if err := ctx.Err(); err != nil {
		stopProcess()
		return nil, context.Cause(ctx)
	}
	if err := cmd.Start(); err != nil {
		stopProcess()
		return nil, fmt.Errorf("start Xray process: %w", err)
	}
	run = &Session{cmd: cmd, api: api, directory: directory, output: output,
		context: processCtx, cancel: stopProcess, done: make(chan struct{}),
		probeTimeout: l.options.ReadyTimeout,
		observation: instance.ProcessObservation{PID: cmd.Process.Pid, Version: version,
			VersionObservedAt: versionObservedAt, StartedAt: time.Now().UTC()}}
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
