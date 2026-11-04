package xray

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/papilio-platform/Node/internal/configuration"
	"github.com/papilio-platform/Node/internal/instance"
)

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
	prepared, err := l.prepareConfiguration()
	if err != nil {
		return err
	}
	defer func() {
		if cleanupErr := os.RemoveAll(prepared.directory); cleanupErr != nil {
			err = errors.Join(err, instance.ErrCleanupIncomplete, cleanupErr)
		}
		if ctx.Err() != nil {
			err = errors.Join(err, context.Cause(ctx))
		}
	}()
	return l.checker.check(ctx, l.config, prepared.path)
}

type preparedConfiguration struct{ directory, path, socket string }

func (l *Launcher) prepareConfiguration() (prepared preparedConfiguration, err error) {
	directory, err := os.MkdirTemp(l.options.RuntimeDir, "xray-")
	if err != nil {
		return prepared, fmt.Errorf("create Xray runtime directory: %w", err)
	}
	prepared.directory = directory
	defer func() {
		if err != nil {
			if cleanupErr := os.RemoveAll(directory); cleanupErr != nil {
				err = errors.Join(err, instance.ErrCleanupIncomplete, cleanupErr)
			}
		}
	}()
	prepared.socket = filepath.Join(directory, "api.sock")
	if len(prepared.socket) > 107 {
		return prepared, errors.New("Xray private socket path exceeds the Linux path limit")
	}
	overlay, err := prepareAPIOverlay(l.config, prepared.socket)
	if err != nil {
		return prepared, fmt.Errorf("prepare Xray API configuration: %w", err)
	}
	prepared.path = filepath.Join(directory, "api.json")
	if err := os.WriteFile(prepared.path, overlay, 0600); err != nil {
		return prepared, fmt.Errorf("write Xray API configuration: %w", err)
	}
	return prepared, nil
}
