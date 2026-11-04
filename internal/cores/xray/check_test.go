package xray_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/cores/xray"
)

func checkerOptions(t *testing.T) xray.CheckerOptions {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return xray.CheckerOptions{
		Executable: executable, WorkingDir: t.TempDir(),
		MaxConfigBytes: 4096, MaxDiagnosticBytes: 64,
		Timeout: 5 * time.Second, WaitDelay: time.Second,
	}
}

func newChecker(t *testing.T, options xray.CheckerOptions) *xray.Checker {
	t.Helper()
	checker, err := xray.NewChecker(options)
	if err != nil {
		t.Fatal(err)
	}
	return checker
}

func TestCheckerRejectsUnboundedOrAmbientOptions(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*xray.CheckerOptions)
	}{
		{"path-search", func(o *xray.CheckerOptions) { o.Executable = "xray" }},
		{"ambient-cwd", func(o *xray.CheckerOptions) { o.WorkingDir = "" }},
		{"relative-assets", func(o *xray.CheckerOptions) { o.AssetsDir = "assets" }},
		{"unbounded-input", func(o *xray.CheckerOptions) { o.MaxConfigBytes = 0 }},
		{"negative-output", func(o *xray.CheckerOptions) { o.MaxDiagnosticBytes = -1 }},
		{"unbounded-runtime", func(o *xray.CheckerOptions) { o.Timeout = 0 }},
		{"unbounded-pipes", func(o *xray.CheckerOptions) { o.WaitDelay = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := checkerOptions(t)
			test.change(&options)
			if _, err := xray.NewChecker(options); err == nil {
				t.Fatal("unsafe or unbounded options accepted")
			}
		})
	}
}

func TestCheckerDoesNotProbeExecutableDuringConstruction(t *testing.T) {
	options := checkerOptions(t)
	options.Executable = filepath.Join(t.TempDir(), "missing")
	checker := newChecker(t, options)
	if err := checker.Check(context.Background(), []byte(`{}`)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing executable error = %v", err)
	}
}

func TestCheckerBoundsInputBeforeExecution(t *testing.T) {
	options := checkerOptions(t)
	options.Executable = filepath.Join(t.TempDir(), "missing")
	checker := newChecker(t, options)
	err := checker.Check(context.Background(), make([]byte, options.MaxConfigBytes+1))
	if !errors.Is(err, xray.ErrConfigTooLarge) {
		t.Fatalf("oversized configuration error = %v", err)
	}

	options = checkerOptions(t)
	config := helperConfig(t, helperRequest{Mode: "success"})
	options.MaxConfigBytes = len(config)
	if err := newChecker(t, options).Check(context.Background(), config); err != nil {
		t.Fatalf("configuration at the byte limit: %v", err)
	}
}

func TestCheckerUsesExplicitEnvironment(t *testing.T) {
	t.Setenv("XRAY_LOCATION_CONFDIR", "/untrusted")
	t.Setenv("xray.location.config", "/untrusted/config.json")
	t.Setenv("PAPILIO_TEST_SECRET", "must-not-reach-child")
	for _, assets := range []bool{false, true} {
		t.Run(strconv.FormatBool(assets), func(t *testing.T) {
			options := checkerOptions(t)
			if assets {
				options.AssetsDir = t.TempDir()
			}
			request := helperRequest{Mode: "environment", WorkingDir: options.WorkingDir, AssetsDir: options.AssetsDir}
			if err := newChecker(t, options).Check(context.Background(), helperConfig(t, request)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCheckerBoundsSensitiveDiagnosticsAndPreservesExit(t *testing.T) {
	options := checkerOptions(t)
	err := newChecker(t, options).Check(context.Background(), helperConfig(t, helperRequest{Mode: "output"}))
	var failure *xray.CheckError
	var exit *exec.ExitError
	if !errors.As(err, &failure) || !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("expected inspectable native exit 23, got %v", err)
	}
	if !failure.Truncated || len(failure.Diagnostics) != options.MaxDiagnosticBytes || !strings.HasSuffix(string(failure.Diagnostics), "private-tail") {
		t.Fatalf("unexpected diagnostic tail: %q, truncated=%v", failure.Diagnostics, failure.Truncated)
	}
	if strings.Contains(err.Error(), "private-tail") {
		t.Fatal("sensitive native output leaked into the default error")
	}
}

func TestCheckerPreservesPreexistingCancellation(t *testing.T) {
	options := checkerOptions(t)
	options.Executable = filepath.Join(t.TempDir(), "missing")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := newChecker(t, options).Check(ctx, []byte(`{}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled operation started execution: %v", err)
	}
}

func TestCheckerKeepsConcurrentDiagnosticsIndependent(t *testing.T) {
	checker := newChecker(t, checkerOptions(t))
	clean := helperConfig(t, helperRequest{Mode: "success"})
	noisy := helperConfig(t, helperRequest{Mode: "output"})
	results := make(chan error, 4)
	for i := range 4 {
		go func() {
			if i%2 == 0 {
				results <- checker.Check(context.Background(), clean)
				return
			}
			err := checker.Check(context.Background(), noisy)
			var failure *xray.CheckError
			if !errors.As(err, &failure) || !strings.HasSuffix(string(failure.Diagnostics), "private-tail") {
				results <- fmt.Errorf("independent failure diagnostics lost: %v", err)
				return
			}
			results <- nil
		}()
	}
	for range 4 {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
}
