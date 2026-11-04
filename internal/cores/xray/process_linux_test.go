package xray_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/cores/xray"
	"github.com/papilio-platform/Node/internal/enginelog"
)

func launchOptions(t *testing.T) xray.LaunchOptions {
	t.Helper()
	root, err := os.MkdirTemp("", "papilio-launch-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	return xray.LaunchOptions{Check: checkerOptions(t), RuntimeDir: root,
		ReadyTimeout: 500 * time.Millisecond, StopTimeout: 100 * time.Millisecond}
}

func TestRuntimeFailureFlushesSensitiveUnterminatedLog(t *testing.T) {
	options := launchOptions(t)
	logs, err := enginelog.New(enginelog.Limits{Records: 2, LineBytes: 64, Readers: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	options.Logs = logs
	launcher, err := xray.NewLauncher(t.Context(), options, helperConfig(t, helperRequest{Mode: "exit-after-validation"}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = launcher.Start(t.Context())
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 42 || strings.Contains(err.Error(), "private-exit-tail") {
		t.Fatalf("runtime error privacy/cause lost: %v", err)
	}
	records, err := logs.Recent(2)
	if err != nil || len(records) != 1 || !records[0].Truncated || len(records[0].Text) != 64 || records[0].Source != enginelog.Stderr || records[0].Execution.PID <= 0 {
		t.Fatalf("bounded failed-start EOF output missing: %+v %v", records, err)
	}
	assertRuntimeEmpty(t, options.RuntimeDir)
}

func TestCapturedValidationUsesEffectiveAPIAndPrivateCleanup(t *testing.T) {
	options := launchOptions(t)
	captured := helperConfig(t, helperRequest{Mode: "output"})
	launcher, err := xray.NewLauncher(t.Context(), options, captured)
	if err != nil {
		t.Fatal(err)
	}
	source := launcher.Source()
	source[0] = 'x'
	if string(launcher.Source()) != string(captured) {
		t.Fatal("captured source is mutable")
	}
	err = launcher.Validate(t.Context())
	var rejected *xray.CheckError
	if !errors.As(err, &rejected) || !rejected.Truncated || len(rejected.Diagnostics) != options.Check.MaxDiagnosticBytes || strings.Contains(err.Error(), "private-tail") {
		t.Fatalf("effective native validation bypassed: %v", err)
	}
	assertRuntimeEmpty(t, options.RuntimeDir)
	valid, err := xray.NewLauncher(t.Context(), options, helperConfig(t, helperRequest{Mode: "validate-with-overlay"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := valid.Validate(t.Context()); err != nil {
		t.Fatal("check did not consume owned effective overlay", err)
	}
	assertRuntimeEmpty(t, options.RuntimeDir)
}

func TestCapturedValidationHonorsCancelledLifetimeBeforeIO(t *testing.T) {
	options := launchOptions(t)
	lifetime, cancel := context.WithCancel(t.Context())
	cancel()
	launcher, err := xray.NewLauncher(lifetime, options, helperConfig(t, helperRequest{Mode: "output"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := launcher.Validate(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatal("validation ignored lifetime cancellation", err)
	}
	assertRuntimeEmpty(t, options.RuntimeDir)
}

func TestCapturedValidationDeadlineReapsChildAndFiles(t *testing.T) {
	options := launchOptions(t)
	ready := filepath.Join(options.Check.WorkingDir, "validation-pid")
	launcher, err := xray.NewLauncher(t.Context(), options, helperConfig(t, helperRequest{Mode: "block", ReadyFile: ready}))
	if err != nil {
		t.Fatal(err)
	}
	if err := launcher.Validate(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("native validation deadline lost: %v", err)
	}
	data, err := os.ReadFile(ready)
	if err != nil {
		t.Fatal("controlled native validation never executed", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatal("validation child remained after return", err)
	}
	assertRuntimeEmpty(t, options.RuntimeDir)
}

func assertRuntimeEmpty(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Errorf("owned runtime resources remain: %v, %v", entries, err)
	}
}

func TestLauncherRejectsUnsafeBoundsBeforeIO(t *testing.T) {
	for _, change := range []func(*xray.LaunchOptions){
		func(o *xray.LaunchOptions) { o.RuntimeDir = "relative" },
		func(o *xray.LaunchOptions) { o.ReadyTimeout = 0 },
		func(o *xray.LaunchOptions) { o.StopTimeout = 0 },
	} {
		options := launchOptions(t)
		change(&options)
		if _, err := xray.NewLauncher(context.Background(), options, []byte(`{}`)); err == nil {
			t.Fatal("unsafe lifecycle bound accepted")
		}
	}
	options := launchOptions(t)
	if _, err := xray.NewLauncher(context.Background(), options, make([]byte, options.Check.MaxConfigBytes+1)); !errors.Is(err, xray.ErrConfigTooLarge) {
		t.Fatalf("oversized captured input: %v", err)
	}
}

func TestLauncherDetectsEarlyExitAndCleansPrivateFiles(t *testing.T) {
	options := launchOptions(t)
	options.ReadyTimeout = 30 * time.Second
	config := helperConfig(t, helperRequest{Mode: "exit-after-validation"})
	launcher, err := xray.NewLauncher(context.Background(), options, config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	_, err = launcher.Start(ctx)
	var exit *exec.ExitError
	var failure *xray.ProcessError
	if !errors.As(err, &exit) || exit.ExitCode() != 42 || !errors.As(err, &failure) {
		t.Fatalf("early exit cause lost: %v", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("confirmed early exit waited for the readiness/caller deadline")
	}
	if !failure.Truncated || len(failure.Diagnostics) != options.Check.MaxDiagnosticBytes || !strings.HasSuffix(string(failure.Diagnostics), "private-exit-tail") || strings.Contains(err.Error(), "private-exit-tail") {
		t.Fatal("unbounded, missing or publicly exposed process diagnostics")
	}
	assertRuntimeEmpty(t, options.RuntimeDir)
}

func TestLauncherReadinessDeadlineKillsAndReapsUncooperativeChild(t *testing.T) {
	options := launchOptions(t)
	ready := filepath.Join(options.Check.WorkingDir, "ready")
	config := helperConfig(t, helperRequest{Mode: "block-after-validation", ReadyFile: ready})
	launcher, err := xray.NewLauncher(context.Background(), options, config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = launcher.Start(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("readiness timeout cause lost: %v", err)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("uncooperative child was not forcibly reaped: %v", err)
	}
	data, err := os.ReadFile(ready)
	if err != nil {
		t.Fatal("borrowed working directory was removed or child never started:", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid child identity %q: %v", data, err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("child still exists after failed readiness: %v", err)
	}
	assertRuntimeEmpty(t, options.RuntimeDir)
}

func TestLauncherCancelledLifetimeDoesNotStart(t *testing.T) {
	options := launchOptions(t)
	options.Check.Executable = filepath.Join(options.Check.WorkingDir, "missing")
	lifetime, cancel := context.WithCancel(context.Background())
	cancel()
	launcher, err := xray.NewLauncher(lifetime, options, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := launcher.Start(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lifetime attempted startup: %v", err)
	}
	assertRuntimeEmpty(t, options.RuntimeDir)
}

func TestLauncherCancellationJoinsStartedAttempt(t *testing.T) {
	options := launchOptions(t)
	options.ReadyTimeout = 5 * time.Second
	ready := filepath.Join(options.Check.WorkingDir, "ready")
	launcher, err := xray.NewLauncher(context.Background(), options,
		helperConfig(t, helperRequest{Mode: "block-after-validation", ReadyFile: ready}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := launcher.Start(ctx)
		done <- err
	}()
	joined := false
	defer func() {
		cancel()
		if !joined {
			<-done
		}
	}()
	var pid int
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		if data, err := os.ReadFile(ready); err == nil {
			pid, _ = strconv.Atoi(string(data))
			if pid > 0 {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if pid <= 0 {
		t.Fatal("child did not acknowledge startup")
	}
	cancel()
	err = <-done
	joined = true
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("startup cancellation cause lost: %v", err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("cancelled startup left an owned process: %v", err)
	}
	assertRuntimeEmpty(t, options.RuntimeDir)
}

func TestSessionPreservesNonGracefulTerminationAfterAPIReadiness(t *testing.T) {
	options := launchOptions(t)
	options.ReadyTimeout = 5 * time.Second
	lifetime, cancelLifetime := context.WithCancel(context.Background())
	defer cancelLifetime()
	launcher, err := xray.NewLauncher(lifetime, options,
		helperConfig(t, helperRequest{Mode: "api-with-default-signal"}))
	if err != nil {
		t.Fatal(err)
	}
	session, err := launcher.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make(chan error, 4)
	for range 4 {
		go func() { results <- session.Stop(ctx) }()
	}
	for range 4 {
		err := <-results
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGTERM {
			t.Errorf("non-graceful termination was hidden: %v", err)
		}
	}
	if err := syscall.Kill(session.PID(), 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("non-graceful session was not reaped: %v", err)
	}
	assertRuntimeEmpty(t, options.RuntimeDir)
}
