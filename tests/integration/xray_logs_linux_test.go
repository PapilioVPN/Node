package integration_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/enginelog"
	"github.com/papilio-platform/Node/internal/instance"
)

func nativeLogs(t testing.TB) *enginelog.Buffer {
	t.Helper()
	b, err := enginelog.New(enginelog.Limits{Records: 256, LineBytes: 1024, Readers: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return b
}

func awaitLog(t *testing.T, s *enginelog.Subscription, pid int, fragment string) enginelog.Record {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		d, err := s.Next(ctx)
		if err != nil {
			t.Fatalf("missing native log %q: %v", fragment, err)
		}
		if d.Skipped != 0 {
			t.Fatalf("unexpected test log loss: %d", d.Skipped)
		}
		if d.Record.Execution.PID == pid && strings.Contains(d.Record.Text, fragment) {
			return d.Record
		}
	}
}

func TestXrayLogsReplayLiveTrafficAndRestart(t *testing.T) {
	logs := nativeLogs(t)
	root, err := os.MkdirTemp("", "papilio-logs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	socket := filepath.Join(root, "http.sock")
	config := fmt.Appendf(nil, `{"log":{"loglevel":"info","access":""},"inbounds":[{"listen":%q,"protocol":"http","settings":{}}],"outbounds":[{"protocol":"freedom"}]}`, socket)
	launcher, _, _ := nativeLauncher(t, config, logs)
	service, err := instance.New("logs", "xray", launcher)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := service.Stop(ctx)
		assertNativeShutdown(t, err)
	})
	started, err := service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	s, err := logs.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := awaitLog(t, s, started.Process.PID, "started")
	if r.Level != enginelog.Warning || r.Source != enginelog.Stdout || r.Execution.StartedAt != started.Process.StartedAt {
		t.Fatalf("native source/level/run identity missing: %+v", r)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "native logs") }))
	defer target.Close()
	transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "owned-xray.invalid:8080"}), DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}, MaxConnsPerHost: 1}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	for cycle := range 2 {
		response, err := client.Get(target.URL)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 64))
		response.Body.Close()
		if readErr != nil || string(body) != "native logs" {
			t.Fatal("forwarding failed", readErr)
		}
		r = awaitLog(t, s, started.Process.PID, "accepted "+target.URL)
		if r.Source != enginelog.Stdout || r.Level != enginelog.Unknown {
			t.Fatalf("access output acquired false severity/source: %+v", r)
		}
		if cycle == 0 {
			replacement, err := service.Restart(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			assertNativeShutdown(t, replacement.StopFailure)
			started = replacement.Current
			transport.CloseIdleConnections()
			awaitLog(t, s, started.Process.PID, "started")
		}
	}
	stopped, err := service.Stop(t.Context())
	assertNativeShutdown(t, err)
	assertInstanceReleased(t, stopped)
	logs.Close()
	for {
		_, err := s.Next(t.Context())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	replay, err := logs.Subscribe(r.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	for {
		d, err := replay.Next(t.Context())
		if err == io.EOF {
			break
		}
		if err != nil || d.Record.Sequence <= r.Sequence {
			t.Fatalf("invalid post-close cursor replay: %+v %v", d, err)
		}
	}
}

func TestXrayCapturePreservesNativeLogDestination(t *testing.T) {
	for _, mode := range []string{"file", "none", "capture-closed"} {
		t.Run(mode, func(t *testing.T) {
			logs := nativeLogs(t)
			path := filepath.Join(t.TempDir(), "native.log")
			config := []byte(`{"log":{"loglevel":"none"},"outbounds":[{"protocol":"freedom"}]}`)
			if mode == "file" {
				config = fmt.Appendf(nil, `{"log":{"loglevel":"info","error":%q,"access":"none"},"outbounds":[{"protocol":"freedom"}]}`, path)
			}
			if mode == "capture-closed" {
				logs.Close()
			}
			launcher, _, _ := nativeLauncher(t, config, logs)
			session, err := launcher.Start(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				assertNativeShutdown(t, session.Stop(ctx))
			})
			if err := session.Probe(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertNativeShutdown(t, session.Stop(t.Context()))
			records, _ := logs.Recent(256)
			for _, r := range records {
				if strings.Contains(r.Text, "Xray 26.10.31 started") {
					t.Fatal("capture changed disabled/file native logging")
				}
			}
			if mode == "capture-closed" && len(records) != 0 {
				t.Fatal("closed capture retained output")
			}
			if mode == "file" {
				data, err := os.ReadFile(path)
				if err != nil || !strings.Contains(string(data), "Xray 26.10.31 started") {
					t.Fatalf("native file logging lost: %v", err)
				}
			}
		})
	}
}
