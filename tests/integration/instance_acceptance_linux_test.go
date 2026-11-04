package integration_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/cores/xray"
	"github.com/papilio-platform/Node/internal/instance"
)

func nativeInstance(t testing.TB, config []byte) (*instance.Service, context.CancelFunc, string) {
	t.Helper()
	launcher, shutdown, root := nativeLauncher(t, config)
	service, err := instance.New("primary", "xray", launcher)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := service.Stop(ctx)
		assertNativeShutdown(t, err)
	})
	return service, shutdown, root
}

func assertInstanceReleased(t testing.TB, status instance.Snapshot) {
	t.Helper()
	if status.Phase != instance.PhaseStopped || status.Process.Exit == nil || !status.Process.CleanupComplete || status.Process.CleanupFailure != nil {
		t.Fatalf("instance resources were not confirmed released: %+v", status)
	}
	if err := syscall.Kill(status.Process.PID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("owned child still exists after cleanup: %v", err)
	}
}

func TestInstanceRejectsRealInvalidConfiguration(t *testing.T) {
	const invalidProtocol = "papilio-invalid-protocol"
	service, _, root := nativeInstance(t, []byte(`{"outbounds":[{"protocol":"`+invalidProtocol+`"}]}`))
	status, err := service.Start(t.Context())
	var nativeExit *exec.ExitError
	var rejected *xray.CheckError
	if !errors.As(err, &rejected) || !errors.As(err, &nativeExit) || nativeExit.ExitCode() != 23 {
		t.Fatalf("real validation rejection was lost at the application boundary: %v", err)
	}
	if status.Phase != instance.PhaseFailed || status.AppliedRevision != "" || status.Process.PID != 0 || !errors.Is(status.LastFailure, rejected) {
		t.Fatalf("invalid configuration claimed an applied execution: %+v", status)
	}
	if len(rejected.Diagnostics) == 0 || len(rejected.Diagnostics) > 4096 || strings.Contains(err.Error(), invalidProtocol) {
		t.Fatal("native rejection diagnostics were absent, unbounded or publicly disclosed")
	}
	if _, err := service.Start(t.Context()); !errors.Is(err, instance.ErrFailed) {
		t.Fatal("failed configuration retried implicitly")
	}
	status, err = service.Stop(t.Context())
	if err != nil || status.Phase != instance.PhaseStopped || status.AppliedRevision != "" {
		t.Fatalf("failed validation could not be acknowledged: %+v, %v", status, err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("native rejection retained owned files: %v, %v", entries, err)
	}
}

func TestInstanceRecoversAfterRealListenerConflict(t *testing.T) {
	borrowed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { borrowed.Close() })
	port := borrowed.Addr().(*net.TCPAddr).Port
	config := fmt.Appendf(nil, `{"log":{"loglevel":"warning"},"inbounds":[{"listen":"127.0.0.1","port":%d,"protocol":"socks","settings":{"auth":"noauth","udp":false}}],"outbounds":[{"protocol":"freedom"}]}`, port)
	service, _, root := nativeInstance(t, config)
	marker := filepath.Join(root, "operator-owned")
	if err := os.WriteFile(marker, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	status, err := service.Start(t.Context())
	var nativeExit *exec.ExitError
	var failure *xray.ProcessError
	if !errors.As(err, &nativeExit) || nativeExit.ExitCode() != 255 || !errors.As(err, &failure) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("native listener conflict was confused with a readiness timeout: %v", err)
	}
	if status.Phase != instance.PhaseFailed || status.AppliedRevision != "" || len(failure.Diagnostics) == 0 || len(failure.Diagnostics) > 4096 {
		t.Fatalf("rejected native execution was applied or lost diagnostics: %+v", status)
	}
	connection, err := net.DialTimeout("tcp", borrowed.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("failed startup closed a borrowed listener: %v", err)
	}
	connection.Close()
	if _, err := service.Start(t.Context()); !errors.Is(err, instance.ErrFailed) {
		t.Fatal("runtime failure restarted without recovery")
	}
	if _, err := service.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	status, err = service.Start(t.Context())
	if err != nil || status.Phase != instance.PhaseReady || status.AppliedRevision != status.DesiredRevision || status.LastFailure != nil {
		t.Fatalf("explicit recovery failed after releasing the conflict: %+v, %v", status, err)
	}
	stopped, err := service.Stop(t.Context())
	assertNativeShutdown(t, err)
	assertInstanceReleased(t, stopped)
	if stopped.AppliedRevision != status.AppliedRevision {
		t.Fatal("recovery cleanup erased the committed revision")
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 1 || entries[0].Name() != "operator-owned" {
		t.Fatalf("cleanup retained runtime resources or removed borrowed files: %v, %v", entries, err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "preserve" {
		t.Fatal("cleanup changed the borrowed operator file")
	}
}

func TestInstanceProcessLifetimeTerminatesRealEngine(t *testing.T) {
	service, shutdown, root := nativeInstance(t,
		[]byte(`{"log":{"loglevel":"none"},"outbounds":[{"protocol":"freedom"}]}`))
	status, err := service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	shutdown()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for !service.Status().Process.CleanupComplete {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("process lifetime cancellation did not finish native cleanup")
		}
	}
	if failed := service.Status(); failed.Phase != instance.PhaseFailed || !errors.Is(failed.LastFailure, instance.ErrUnexpectedExit) || failed.AppliedRevision != status.AppliedRevision {
		t.Fatalf("owner lifetime exit was confused with an admitted Stop: %+v", failed)
	}
	stopped, err := service.Stop(ctx)
	assertNativeShutdown(t, err)
	assertInstanceReleased(t, stopped)
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("owner lifetime retained native resources: %v, %v", entries, err)
	}
	status, err = service.Start(ctx)
	if !errors.Is(err, context.Canceled) || status.Phase != instance.PhaseFailed || status.AppliedRevision != stopped.AppliedRevision {
		t.Fatalf("ended owner lifetime admitted a new native process: %+v, %v", status, err)
	}
}
