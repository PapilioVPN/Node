package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/instance"
)

func TestInstanceProxiesLocalTrafficAcrossLifecycleCycles(t *testing.T) {
	pinnedXray(t)
	fixture, err := os.MkdirTemp("", "papilio-traffic-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(fixture) })
	socket := filepath.Join(fixture, "proxy.sock")
	config := fmt.Appendf(nil, `{"log":{"loglevel":"none"},"inbounds":[{"listen":%q,"protocol":"http","settings":{}}],"outbounds":[{"protocol":"freedom"}]}`, socket)
	service, _, runtimeRoot := nativeInstance(t, config)
	var received atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		_, _ = io.Copy(w, http.MaxBytesReader(w, r.Body, 4096))
	}))
	t.Cleanup(target.Close)
	transport := &http.Transport{
		Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "owned-xray.invalid:8080"}),
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
		MaxConnsPerHost: 1, MaxIdleConnsPerHost: 1, IdleConnTimeout: time.Second,
	}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	payload := bytes.Repeat([]byte("Papilio Node acceptance\x00\xff\n"), 64)
	for cycle := range 3 {
		request, cancelRequest := context.WithTimeout(t.Context(), 10*time.Second)
		started, err := service.Start(request)
		cancelRequest()
		if err != nil {
			t.Fatal(err)
		}
		reused, err := service.Start(t.Context())
		if err != nil || reused.Process.PID != started.Process.PID || reused.Phase != instance.PhaseReady || reused.AppliedRevision != started.DesiredRevision {
			t.Fatalf("completed start request changed the native session: %+v, %v", reused, err)
		}
		response, err := client.Post(target.URL, "application/octet-stream", bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("cycle %d: real Xray did not forward the local request: %v", cycle, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || !bytes.Equal(body, payload) || received.Load() != int64(cycle+1) {
			t.Fatalf("cycle %d: proxy payload/outcome mismatch: status %d, read %v, close %v", cycle, response.StatusCode, readErr, closeErr)
		}
		stopped, err := service.Stop(t.Context())
		assertNativeShutdown(t, err)
		assertInstanceReleased(t, stopped)
		if stopped.AppliedRevision != started.AppliedRevision {
			t.Fatal("stop erased the successfully executed revision")
		}
		transport.CloseIdleConnections()
		response, err = client.Post(target.URL, "application/octet-stream", bytes.NewReader(payload))
		if err == nil {
			response.Body.Close()
			t.Fatal("request still reached its target after the owned proxy stopped")
		}
		if received.Load() != int64(cycle+1) {
			t.Fatal("stopped-proxy check bypassed the owned engine")
		}
		if entries, err := os.ReadDir(runtimeRoot); err != nil || len(entries) != 0 {
			t.Fatalf("cycle %d: owned runtime resources accumulated: %v, %v", cycle, entries, err)
		}
	}
}
