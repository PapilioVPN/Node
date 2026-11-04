package integration_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/cores/xray"
)

func nativeLauncher(t testing.TB, config []byte) (*xray.Launcher, context.CancelFunc, string) {
	t.Helper()
	executable := pinnedXray(t)
	root, err := os.MkdirTemp("", "papilio-%#?-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	lifetime, shutdown := context.WithCancel(context.Background())
	t.Cleanup(shutdown)
	launcher, err := xray.NewLauncher(lifetime, xray.LaunchOptions{
		Check: xray.CheckerOptions{
			Executable: executable, WorkingDir: t.TempDir(),
			MaxConfigBytes: 1 << 20, MaxDiagnosticBytes: 4096,
			Timeout: 5 * time.Second, WaitDelay: time.Second,
		},
		RuntimeDir: root, ReadyTimeout: 5 * time.Second, StopTimeout: time.Second,
	}, config)
	if err != nil {
		t.Fatal(err)
	}
	return launcher, shutdown, root
}

func assertNativeShutdown(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ProcessState.Sys().(syscall.WaitStatus).Signal() == syscall.SIGTERM {
		t.Log("Xray exited before installing its SIGTERM handler; native signal failure remains inspectable")
		return
	}
	t.Fatal(err)
}

func TestXrayOwnedSession(t *testing.T) {
	config := []byte(`{
		"log":{"loglevel":"none"},
		"api":{"tag":"existing-api","listen":"127.0.0.1:0","services":["HandlerService"]},
		"stats":{},"outbounds":[{"tag":"direct","protocol":"freedom"}]
	}`)
	launcher, _, root := nativeLauncher(t, config)
	clear(config) // The launcher owns its captured input, not this caller's slice.
	request, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	session, err := launcher.Start(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		assertNativeShutdown(t, session.Stop(ctx))
	})
	pid := session.PID()
	if pid <= 0 {
		t.Fatal("missing owned process identity")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one private run directory: %v, %v", entries, err)
	}
	directory := filepath.Join(root, entries[0].Name())
	for path, mode := range map[string]os.FileMode{directory: 0700, filepath.Join(directory, "api.json"): 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("unsafe owned runtime permissions for %s: %v, %v", path, info, err)
		}
	}
	cancelRequest()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := session.Probe(ctx); err != nil {
		t.Fatalf("successful session still depended on its completed start request: %v", err)
	}
	assertNativeShutdown(t, session.Stop(ctx))
	if err := session.Probe(ctx); !errors.Is(err, xray.ErrSessionExited) {
		t.Fatalf("completed session did not report its exit: %v", err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("owned child %d still exists after Stop: %v", pid, err)
	}
	entries, err = os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("owned socket/directory cleanup incomplete: %v, %v", entries, err)
	}
}

func TestXrayProcessLifetimeOwnsMinimalSession(t *testing.T) {
	launcher, shutdown, root := nativeLauncher(t,
		[]byte(`{"log":{"loglevel":"none"},"outbounds":[{"protocol":"freedom"}]}`))
	session, err := launcher.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	assertNativeShutdown(t, session.Wait(ctx))
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("lifetime cleanup incomplete: %v, %v", entries, err)
	}
}

func TestXrayStopContinuesAfterCallerCancellation(t *testing.T) {
	launcher, _, _ := nativeLauncher(t,
		[]byte(`{"log":{"loglevel":"none"},"outbounds":[{"protocol":"freedom"}]}`))
	session, err := launcher.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	caller, cancelCaller := context.WithCancel(context.Background())
	cancelCaller()
	if err := session.Stop(caller); err != nil && !errors.Is(err, context.Canceled) {
		assertNativeShutdown(t, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	assertNativeShutdown(t, session.Wait(ctx))
}

func TestXrayInvalidStartupHasNoOwnedRuntime(t *testing.T) {
	launcher, _, root := nativeLauncher(t, []byte(`{"outbounds":[{"protocol":"papilio-invalid"}]}`))
	if session, err := launcher.Start(context.Background()); err == nil || session != nil {
		t.Fatalf("invalid configuration started a session: %v, %v", session, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid configuration left owned files: %v, %v", entries, err)
	}
}

func BenchmarkXrayLifecycle(b *testing.B) {
	launcher, _, _ := nativeLauncher(b,
		[]byte(`{"log":{"loglevel":"none"},"outbounds":[{"protocol":"freedom"}]}`))
	b.ReportAllocs()
	for b.Loop() {
		session, err := launcher.Start(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		if err := session.Stop(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}
