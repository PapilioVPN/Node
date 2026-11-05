package integration_test

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/instance"
	"github.com/papilio-platform/Node/internal/users"
)

func TestAccountMutationsControlRealAuthenticatedTraffic(t *testing.T) {
	for _, family := range []users.Protocol{users.VMess, users.VLESS, users.Trojan} {
		t.Run(fmt.Sprint(family), func(t *testing.T) {
			server, port := startAccountServer(t, family)
			commands := nativeAccountCommands(t, server)
			target := users.Target{Generation: server.Status().Generation, Inbound: "accounts"}
			old := accountInput(family, "primary", 1)
			next := accountInput(family, "primary", 2)
			sibling := accountInput(family, "sibling", 3)
			sockets := startAccountClients(t, port, []users.Account{old, next, sibling})
			clients := []*http.Client{accountHTTPClient(t, sockets[0]), accountHTTPClient(t, sockets[1]), accountHTTPClient(t, sockets[2])}
			var received atomic.Int64
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received.Add(1)
				io.WriteString(w, "authenticated target")
			}))
			defer destination.Close()
			request := func(index int, allowed bool) {
				t.Helper()
				before := received.Load()
				response, err := clients[index].Get(destination.URL)
				var body []byte
				if err == nil {
					body, _ = io.ReadAll(io.LimitReader(response.Body, 64))
					response.Body.Close()
				}
				if allowed {
					if err != nil || string(body) != "authenticated target" || received.Load() != before+1 {
						t.Fatalf("authenticated traffic denied: client%d %v", index, err)
					}
				} else if received.Load() != before || string(body) == "authenticated target" {
					t.Fatalf("revoked credential reached target: client%d", index)
				}
			}
			request(0, false)
			created, err := commands.Create(t.Context(), target, old)
			if err != nil || created.Outcome != users.OutcomeApplied {
				t.Fatal("native create", created, err)
			}
			oldRevision := created.Revision
			request(0, true)
			if r, err := commands.Create(t.Context(), target, old); err != nil || r.Outcome != users.OutcomeUnchanged {
				t.Fatal("native replay", r, err)
			}
			if _, err := commands.Create(t.Context(), target, next); !errors.Is(err, users.ErrConflict) {
				t.Fatal("native identity overwrite", err)
			}
			alias := old
			alias.Email = "alias"
			if family == users.VLESS {
				alias.ID[6] = 0x55
				alias.ID[7] = 0x55
			}
			if _, err := commands.Create(t.Context(), target, alias); !errors.Is(err, users.ErrConflict) {
				t.Fatal("native credential alias", err)
			}
			if _, err := commands.Create(t.Context(), target, sibling); err != nil {
				t.Fatal(err)
			}
			request(2, true)
			replaced, err := commands.Replace(t.Context(), target, oldRevision, next)
			if err != nil || replaced.Outcome != users.OutcomeApplied || !replaced.PreviousRemoved {
				t.Fatal("native replacement", replaced, err)
			}
			newRevision := replaced.Revision
			if _, err := commands.Replace(t.Context(), target, oldRevision, old); !errors.Is(err, users.ErrConflict) {
				t.Fatal("stale account replaced current credential", err)
			}
			if _, err := commands.Remove(t.Context(), target, old.Email, oldRevision); !errors.Is(err, users.ErrConflict) {
				t.Fatal("stale removal revoked newer credential", err)
			}
			request(0, false)
			request(1, true)
			request(2, true)
			if r, err := commands.Replace(t.Context(), target, oldRevision, next); err != nil || r.Outcome != users.OutcomeUnchanged {
				t.Fatal("replacement replay", r, err)
			}
			if r, err := commands.Remove(t.Context(), target, "PRIMARY", newRevision); err != nil || r.Outcome != users.OutcomeAbsent || !r.PreviousRemoved {
				t.Fatal("native remove", r, err)
			}
			request(1, false)
			request(2, true)
			if r, err := commands.Remove(t.Context(), target, old.Email, newRevision); err != nil || r.PreviousRemoved {
				t.Fatal("absent remove replay", r, err)
			}
			if _, err := commands.Replace(t.Context(), target, newRevision, old); !errors.Is(err, users.ErrNotFound) {
				t.Fatal("replacement created absent identity", err)
			}
			restarted, err := server.Restart(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			assertNativeShutdown(t, restarted.StopFailure)
			if _, err := commands.Create(t.Context(), target, old); !errors.Is(err, instance.ErrExecutionConflict) {
				t.Fatal("stale write reached new execution", err)
			}
			target.Generation = restarted.Current.Generation
			request(2, false)
			if _, err := commands.Create(t.Context(), target, old); err != nil {
				t.Fatal("explicit replay after restart", err)
			}
			request(0, true)
		})
	}
}

func TestAccountRemovalDoesNotTerminateEstablishedStream(t *testing.T) {
	for _, family := range []users.Protocol{users.VMess, users.VLESS, users.Trojan} {
		t.Run(fmt.Sprint(family), func(t *testing.T) {
			server, port := startAccountServer(t, family)
			commands := nativeAccountCommands(t, server)
			target := users.Target{Generation: server.Status().Generation, Inbound: "accounts"}
			account := accountInput(family, "stream", 1)
			created, err := commands.Create(t.Context(), target, account)
			if err != nil {
				t.Fatal(err)
			}
			sockets := startAccountClients(t, port, []users.Account{account})
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				io.Copy(conn, conn)
			}()
			t.Cleanup(func() { listener.Close(); <-done })
			conn, err := net.DialTimeout("unix", sockets[0], time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(4 * time.Second))
			fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", listener.Addr(), listener.Addr())
			reader := bufio.NewReader(conn)
			response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
			if err != nil || response.StatusCode != 200 {
				t.Fatal("stream CONNECT failed", err)
			}
			echo := func() {
				t.Helper()
				if _, err := conn.Write([]byte("probe")); err != nil {
					t.Fatal(err)
				}
				got := make([]byte, 5)
				if _, err := io.ReadFull(reader, got); err != nil || string(got) != "probe" {
					t.Fatal("established stream interrupted", err)
				}
			}
			echo()
			if _, err := commands.Remove(t.Context(), target, account.Email, created.Revision); err != nil {
				t.Fatal(err)
			}
			echo()
		})
	}
}
