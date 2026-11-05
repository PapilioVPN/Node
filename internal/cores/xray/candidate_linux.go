package xray

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/papilio-platform/Node/internal/configuration"
	"github.com/papilio-platform/Node/internal/enginelog"
	"github.com/papilio-platform/Node/internal/instance"
)

// LaunchOptions supplies the native environment and owned runtime limits.
type LaunchOptions struct {
	Check        CheckerOptions
	RuntimeDir   string
	ReadyTimeout time.Duration
	StopTimeout  time.Duration
	// Logs explicitly opts into sensitive console capture. The binding's
	// composition owns this buffer across runs and closes it after joining them.
	Logs *enginelog.Buffer
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

// Revision identifies original bytes and the execution policy, excluding the
// temporary socket path and externally referenced file contents.
func (l *Launcher) Revision() instance.Revision { return l.revision }

// ConfigurationFactory captures fresh launchers using one binding's explicit
// environment and lifetime. Options must remain immutable while in use.
type ConfigurationFactory struct {
	Lifetime context.Context
	Options  LaunchOptions
}

// Capture copies bounded original source without native work.
func (f ConfigurationFactory) Capture(source []byte) (configuration.ICandidate, error) {
	return NewLauncher(f.Lifetime, f.Options, source)
}

// Source returns an independent sensitive copy of the captured original bytes.
func (l *Launcher) Source() []byte { return bytes.Clone(l.config) }

// Validate checks the same source and API preparation as Start without listeners.
// Validation is a point-in-time check; executable/assets can change afterwards.
func (l *Launcher) Validate(ctx context.Context) (err error) {
	ctx, cancelTimeout := context.WithTimeout(ctx, l.options.Check.Timeout)
	defer cancelTimeout()
	ctx, cancel := context.WithCancelCause(ctx)
	stopWatch := context.AfterFunc(l.lifetime, func() { cancel(context.Cause(l.lifetime)) })
	defer stopWatch()
	defer cancel(nil)
	if err := ctx.Err(); err != nil {
		return context.Cause(ctx)
	}
	if err := l.lifetime.Err(); err != nil {
		return context.Cause(l.lifetime)
	}
	prepared, err := prepareRuntimeConfiguration(ctx, l.config, l.options.RuntimeDir, l.checker)
	if err != nil {
		return err
	}
	defer func() {
		if cleanupErr := prepared.files.close(); cleanupErr != nil {
			err = errors.Join(err, instance.ErrCleanupIncomplete, cleanupErr)
		}
		if ctx.Err() != nil {
			err = errors.Join(err, context.Cause(ctx))
		}
	}()
	return l.checker.check(ctx, prepared.input)
}
