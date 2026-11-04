// Package xray implements native Xray engine operations.
package xray

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

// ErrConfigTooLarge means validation was refused before starting a process.
var ErrConfigTooLarge = errors.New("Xray configuration exceeds the byte limit")

// CheckerOptions bounds native validation and selects its explicit environment.
type CheckerOptions struct {
	// Executable and WorkingDir must be absolute. WorkingDir remains caller-owned.
	Executable string
	WorkingDir string
	// AssetsDir is optional and must be absolute when supplied.
	AssetsDir          string
	MaxConfigBytes     int
	MaxDiagnosticBytes int
	// Timeout bounds execution; WaitDelay bounds waiting for inherited I/O pipes.
	Timeout   time.Duration
	WaitDelay time.Duration
}

// Checker validates configuration with a native Xray executable. It is safe for
// concurrent calls; callers must bound admission to their own operations.
type Checker struct {
	options CheckerOptions
}

// NewChecker validates options without filesystem I/O or starting a process.
func NewChecker(options CheckerOptions) (*Checker, error) {
	if !filepath.IsAbs(options.Executable) || !filepath.IsAbs(options.WorkingDir) {
		return nil, errors.New("Xray executable and working directory must be absolute")
	}
	if options.AssetsDir != "" && !filepath.IsAbs(options.AssetsDir) {
		return nil, errors.New("Xray asset directory must be absolute")
	}
	if options.MaxConfigBytes <= 0 || options.MaxDiagnosticBytes <= 0 {
		return nil, errors.New("Xray configuration and diagnostic byte limits must be positive")
	}
	if options.Timeout <= 0 || options.WaitDelay <= 0 {
		return nil, errors.New("Xray validation timeout and pipe wait delay must be positive")
	}
	return &Checker{options: options}, nil
}

// Check borrows config until return and waits for its validation child to exit.
// It inherits no environment, passes config through stdin and retains only a
// bounded diagnostic tail. Native validation is neither readiness nor a sandbox:
// Xray may access resources referenced by config under the caller's privileges.
func (c *Checker) Check(ctx context.Context, config []byte) error {
	return c.check(ctx, config, "")
}

func (c *Checker) check(ctx context.Context, config []byte, overlay string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("check Xray configuration: %w", err)
	}
	if len(config) > c.options.MaxConfigBytes {
		return ErrConfigTooLarge
	}
	ctx, cancel := context.WithTimeout(ctx, c.options.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.options.Executable, "run", "-test", "-format=json", "-c", "stdin:")
	if overlay != "" {
		cmd.Args = append(cmd.Args, "-c", overlay)
	}
	cmd.Dir = c.options.WorkingDir
	cmd.Env = nativeEnvironment(c.options.AssetsDir)
	cmd.Stdin = bytes.NewReader(config)
	output := diagnosticTail{data: make([]byte, c.options.MaxDiagnosticBytes)}
	// os/exec serializes writes when stdout and stderr share the same writer.
	cmd.Stdout, cmd.Stderr = &output, &output
	cmd.WaitDelay = c.options.WaitDelay
	err := cmd.Run()
	if ctx.Err() != nil {
		err = errors.Join(ctx.Err(), err)
	}
	if err != nil {
		return &CheckError{cause: err, Diagnostics: output.bytes(), Truncated: output.truncated}
	}
	return nil
}

// CheckError preserves the native execution cause. Diagnostics contain bounded,
// potentially sensitive engine output and are deliberately excluded from Error.
type CheckError struct {
	Diagnostics []byte
	Truncated   bool
	cause       error
}

func (e *CheckError) Error() string { return fmt.Sprintf("check Xray configuration: %v", e.cause) }

// Unwrap preserves exit, launch and context errors for errors.Is/errors.As.
func (e *CheckError) Unwrap() error { return e.cause }

func nativeEnvironment(assetsDir string) []string {
	environment := []string{}
	if assetsDir != "" {
		environment = append(environment, "XRAY_LOCATION_ASSET="+assetsDir)
	}
	return environment
}
