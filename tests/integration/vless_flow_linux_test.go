package integration_test

import (
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/papilio-platform/Node/internal/instance"
	"github.com/papilio-platform/Node/internal/users"
)

func TestVLESSFlowControlsVerifiedTLS13Traffic(t *testing.T) {
	material := nativeTLSCertificates(t)
	seed := accountInput(users.VLESS, "source-default", 9)
	seed.Flow = users.FlowVision
	server, port := startVLESSServer(t, material, "1.3", seed)
	commands := nativeAccountCommands(t, server)
	target := users.Target{Generation: server.Status().Generation, Inbound: "accounts"}
	plain := accountInput(users.VLESS, "live", 1)
	vision := plain
	vision.Flow = users.FlowVision
	client := func(account users.Account) vlessTestClient {
		return vlessTestClient{account: account, name: "node.test", root: material.root, version: "1.3"}
	}
	wrongName, untrusted := client(vision), client(vision)
	wrongName.name, untrusted.root = "wrong.test", ""
	sockets := startVLESSClients(t, port, []vlessTestClient{client(plain), client(vision), wrongName, untrusted, client(seed)})
	request := secureVLESSProbe(t, material, sockets)
	request(4, true)
	created, err := commands.Create(t.Context(), target, plain)
	if err != nil {
		t.Fatal(err)
	}
	request(0, true) // Live empty flow does not inherit the static source default.
	request(1, false)
	replaced, err := commands.Replace(t.Context(), target, created.Revision, vision)
	if err != nil || !replaced.PreviousRemoved || replaced.Revision == created.Revision {
		t.Fatal("flow-only replacement lost its effect/revision", replaced, err)
	}
	request(0, false)
	request(1, true)
	request(2, false)
	request(3, false)
	request(4, true)
	if _, err := commands.Replace(t.Context(), target, created.Revision, plain); !errors.Is(err, users.ErrConflict) {
		t.Fatal("stale flow overwrote current account", err)
	}
	if replay, err := commands.Replace(t.Context(), target, created.Revision, vision); err != nil || replay.Outcome != users.OutcomeUnchanged || replay.Revision != replaced.Revision {
		t.Fatal("Vision replay changed account", replay, err)
	}
	plainAgain, err := commands.Replace(t.Context(), target, replaced.Revision, plain)
	if err != nil {
		t.Fatal(err)
	}
	request(0, true)
	request(1, false)
	if _, err := commands.Remove(t.Context(), target, plain.Email, plainAgain.Revision); err != nil {
		t.Fatal(err)
	}
	request(0, false)
	if _, err := commands.Create(t.Context(), target, vision); err != nil {
		t.Fatal(err)
	}
	request(1, true)
	restarted, err := server.Restart(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	assertNativeShutdown(t, restarted.StopFailure)
	request(1, false)
	request(4, true)
	if _, err := commands.Create(t.Context(), target, vision); !errors.Is(err, instance.ErrExecutionConflict) {
		t.Fatal("stale generation changed restarted account", err)
	}
}

func TestVLESSVisionRequiresTLS13(t *testing.T) {
	material := nativeTLSCertificates(t)
	seed := accountInput(users.VLESS, "source", 9)
	server, port := startVLESSServer(t, material, "1.2", seed)
	plain := accountInput(users.VLESS, "plain", 1)
	vision := accountInput(users.VLESS, "vision", 2)
	vision.Flow = users.FlowVision
	commands := nativeAccountCommands(t, server)
	target := users.Target{Generation: server.Status().Generation, Inbound: "accounts"}
	for _, account := range []users.Account{plain, vision} {
		if _, err := commands.Create(t.Context(), target, account); err != nil {
			t.Fatal(err)
		}
	}
	sockets := startVLESSClients(t, port, []vlessTestClient{
		{account: plain, name: "node.test", root: material.root, version: "1.2"},
		{account: vision, name: "node.test", root: material.root, version: "1.2"},
	})
	request := secureVLESSProbe(t, material, sockets)
	request(0, true)
	request(1, false)
}

func TestVLESSVisionRejectsPlainTransport(t *testing.T) {
	server, port := startAccountServer(t, users.VLESS)
	plain := accountInput(users.VLESS, "plain", 1)
	vision := accountInput(users.VLESS, "vision", 2)
	vision.Flow = users.FlowVision
	commands := nativeAccountCommands(t, server)
	target := users.Target{Generation: server.Status().Generation, Inbound: "accounts"}
	for _, account := range []users.Account{plain, vision} {
		if _, err := commands.Create(t.Context(), target, account); err != nil {
			t.Fatal(err)
		}
	}
	request := secureVLESSProbe(t, nativeTLSCertificates(t), startAccountClients(t, port, []users.Account{plain, vision}))
	request(0, true)
	request(1, false)
}

// The inner HTTPS exchange verifies its test CA. The target observes receipts;
// a proxy error response is never acceptance.
func secureVLESSProbe(t *testing.T, material nativeTestTLS, sockets []string) func(int, bool) {
	t.Helper()
	payload := strings.Repeat("isolated-vision-payload\n", 8192)
	var received atomic.Int64
	destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		if r.TLS == nil || r.TLS.Version != tls.VersionTLS13 {
			http.Error(w, "inner TLS1.3 required", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, int64(len(payload)+1)))
		if err != nil || string(body) != payload {
			http.Error(w, "payload mismatch", http.StatusBadRequest)
			return
		}
		w.Write(body)
	}))
	destination.TLS = &tls.Config{Certificates: []tls.Certificate{material.certificate}, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	destination.StartTLS()
	t.Cleanup(destination.Close)
	clients := make([]*http.Client, len(sockets))
	for i, socket := range sockets {
		clients[i] = accountHTTPClient(t, socket)
		clients[i].Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: material.roots, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	}
	return func(index int, allowed bool) {
		t.Helper()
		before := received.Load()
		response, err := clients[index].Post(destination.URL, "application/octet-stream", strings.NewReader(payload))
		var body []byte
		if err == nil {
			body, err = io.ReadAll(io.LimitReader(response.Body, int64(len(payload)+1)))
			response.Body.Close()
		}
		if allowed {
			if err != nil || string(body) != payload || received.Load() != before+1 {
				t.Fatalf("verified secure traffic denied: client%d: %v", index, err)
			}
		} else if received.Load() != before || string(body) == payload {
			t.Fatalf("rejected secure client%d reached actual target", index)
		}
	}
}
