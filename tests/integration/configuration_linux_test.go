package integration_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/configuration"
	"github.com/papilio-platform/Node/internal/cores/xray"
	"github.com/papilio-platform/Node/internal/instance"
)

func nativeConfiguration(t testing.TB, source []byte) (*configuration.Service, *instance.Service, string) {
	t.Helper()
	root, err := os.MkdirTemp("", "papilio-config-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	lifetime, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	factory := xray.ConfigurationFactory{Lifetime: lifetime, Options: xray.LaunchOptions{
		Check:      xray.CheckerOptions{Executable: pinnedXray(t), WorkingDir: t.TempDir(), MaxConfigBytes: 1 << 20, MaxDiagnosticBytes: 4096, Timeout: 5 * time.Second, WaitDelay: time.Second},
		RuntimeDir: root, ReadyTimeout: 5 * time.Second, StopTimeout: time.Second,
	}}
	initial, err := factory.Capture(source)
	if err != nil {
		t.Fatal(err)
	}
	life, err := instance.New("primary", "xray", initial)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := life.Stop(ctx)
		assertNativeShutdown(t, err)
	})
	s, err := configuration.New(life, factory, initial)
	if err != nil {
		t.Fatal(err)
	}
	return s, life, root
}

func TestConfigurationControlsActualTrafficAndRejectsInvalidReplacement(t *testing.T) {
	fixture, err := os.MkdirTemp("", "papilio-config-traffic-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(fixture) })
	socket := filepath.Join(fixture, "http.sock")
	source := fmt.Appendf(nil, `{"log":{"loglevel":"none"},"inbounds":[{"listen":%q,"protocol":"http","settings":{}}],"outbounds":[{"protocol":"freedom"}]}`, socket)
	s, life, root := nativeConfiguration(t, source)
	started, err := life.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	validation, err := s.Validate(t.Context(), source)
	if err != nil || validation.RestartRequired || validation.Revision != started.DesiredRevision {
		t.Fatalf("unchanged validation: %+v %v", validation, err)
	}
	var received atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		io.WriteString(w, "configuration verified")
	}))
	defer target.Close()
	transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "owned-xray.invalid:8080"}), DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}, MaxConnsPerHost: 1}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	forwarded := func() {
		t.Helper()
		response, err := client.Get(target.URL)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, 64))
		response.Body.Close()
		if err != nil || string(body) != "configuration verified" {
			t.Fatal("native forwarding lost", err)
		}
	}
	forwarded()
	rejected, err := s.Apply(t.Context(), started.DesiredRevision, []byte(`{"outbounds":[{"protocol":"papilio-invalid"}]}`))
	var check *xray.CheckError
	if !errors.As(err, &check) || rejected.Current.Process.PID != started.Process.PID || rejected.Current.Phase != instance.PhaseReady {
		t.Fatalf("invalid apply disrupted native run: %+v %v", rejected, err)
	}
	forwarded()
	reused, err := s.Apply(t.Context(), started.DesiredRevision, source)
	if err != nil || !reused.Reused || reused.Current.Process.PID != started.Process.PID {
		t.Fatalf("unchanged config restarted native run: %+v %v", reused, err)
	}
	blocked := fmt.Appendf(nil, `{"log":{"loglevel":"none"},"inbounds":[{"listen":%q,"protocol":"http","settings":{}}],"outbounds":[{"protocol":"blackhole"}]}`, socket)
	result, err := s.Apply(t.Context(), started.DesiredRevision, blocked)
	if err != nil {
		t.Fatal(err)
	}
	assertNativeShutdown(t, result.StopFailure)
	assertInstanceReleased(t, result.Previous)
	if result.Current.Process.PID == started.Process.PID || result.Current.AppliedRevision == started.AppliedRevision || result.Current.Phase != instance.PhaseReady {
		t.Fatalf("changed source did not replace execution: %+v", result)
	}
	transport.CloseIdleConnections()
	beforeBlocked := received.Load()
	response, err := client.Get(target.URL)
	if err == nil {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 64))
		response.Body.Close()
		if readErr == nil && string(body) == "configuration verified" {
			t.Fatal("blackhole replacement returned the target payload")
		}
	}
	if beforeBlocked != 2 || received.Load() != beforeBlocked {
		t.Fatal("blackhole replacement still reached the target")
	}
	if _, err := s.Apply(t.Context(), started.DesiredRevision, source); !errors.Is(err, instance.ErrRevisionConflict) {
		t.Fatal("stale request replaced new source", err)
	}
	view, err := s.Read()
	if err != nil || string(view.Source) != string(blocked) {
		t.Fatal("desired source read mismatch", err)
	}
	stopped, err := life.Stop(t.Context())
	assertNativeShutdown(t, err)
	assertInstanceReleased(t, stopped)
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("validation/start retained private directories: %v %v", entries, err)
	}
}

func TestConfigurationRuntimeFailureAndExplicitRecovery(t *testing.T) {
	oldSource := []byte(`{"log":{"loglevel":"none"},"outbounds":[{"protocol":"freedom"}]}`)
	s, life, _ := nativeConfiguration(t, oldSource)
	started, err := life.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	borrowed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer borrowed.Close()
	next := fmt.Appendf(nil, `{"log":{"loglevel":"none"},"inbounds":[{"listen":"127.0.0.1","port":%d,"protocol":"http","settings":{}}],"outbounds":[{"protocol":"freedom"}]}`, borrowed.Addr().(*net.TCPAddr).Port)
	checked, err := s.Validate(t.Context(), next)
	if err != nil || !checked.RestartRequired {
		t.Fatal("native validation confused readiness", err)
	}
	failed, err := s.Apply(t.Context(), started.DesiredRevision, next)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 255 || failed.Current.Phase != instance.PhaseFailed || failed.Current.DesiredRevision != checked.Revision || failed.Current.AppliedRevision != started.AppliedRevision {
		t.Fatalf("runtime rejection facts: %+v %v", failed, err)
	}
	assertNativeShutdown(t, failed.StopFailure)
	assertInstanceReleased(t, failed.Previous)
	view, err := s.Read()
	if err != nil || string(view.Source) != string(next) {
		t.Fatal("failed desired source lost", err)
	}
	borrowed.Close()
	recovered, err := s.Apply(t.Context(), checked.Revision, next)
	if err != nil || recovered.Current.Phase != instance.PhaseReady || recovered.Current.AppliedRevision != checked.Revision {
		t.Fatalf("explicit recovery failed: %+v %v", recovered, err)
	}
}

func BenchmarkConfigurationUnchangedApply(b *testing.B) {
	source := []byte(`{"log":{"loglevel":"none"},"outbounds":[{"protocol":"freedom"}]}`)
	s, life, _ := nativeConfiguration(b, source)
	started, err := life.Start(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		result, err := s.Apply(b.Context(), started.DesiredRevision, source)
		if err != nil || !result.Reused || result.Current.Process.PID != started.Process.PID {
			b.Fatal("unchanged apply did not reuse native session", err)
		}
	}
}
