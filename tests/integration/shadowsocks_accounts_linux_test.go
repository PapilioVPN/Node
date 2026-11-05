package integration_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/cores/xray"
	"github.com/papilio-platform/Node/internal/instance"
	"github.com/papilio-platform/Node/internal/users"
)

func TestLegacyShadowsocksMethodsControlRealTraffic(t *testing.T) {
	methods := []users.ShadowsocksMethod{users.MethodAES128GCM, users.MethodAES256GCM, users.MethodChaCha20Poly1305, users.MethodXChaCha20Poly1305}
	for index, method := range methods {
		t.Run(string(method), func(t *testing.T) {
			server, port := startAccountServer(t, users.Shadowsocks)
			commands := nativeAccountCommands(t, server)
			target := users.Target{Generation: server.Status().Generation, Inbound: "accounts"}
			old := accountInput(users.Shadowsocks, "primary", 1)
			old.Method = method
			changedMethod := old
			changedMethod.Method = methods[(index+1)%len(methods)]
			changedPassword := changedMethod
			changedPassword.Password += "-rotated"
			sibling := accountInput(users.Shadowsocks, "sibling", 3)
			sockets := startAccountClients(t, port, []users.Account{old, changedMethod, changedPassword, sibling})
			request := secureAccountProbe(t, nativeTLSCertificates(t), sockets)
			request(0, false)
			created, err := commands.Create(t.Context(), target, old)
			if err != nil {
				t.Fatal(err)
			}
			request(0, true)
			request(1, false)
			if replay, err := commands.Create(t.Context(), target, old); err != nil || replay.Outcome != users.OutcomeUnchanged || replay.Revision != created.Revision {
				t.Fatal("legacy replay mutated credentials", replay, err)
			}
			alias := old
			alias.Email = "alias"
			if _, err := commands.Create(t.Context(), target, alias); !errors.Is(err, users.ErrConflict) {
				t.Fatal("legacy duplicate authentication key accepted", err)
			}
			if _, err := commands.Create(t.Context(), target, sibling); err != nil {
				t.Fatal(err)
			}
			request(3, true)
			replaced, err := commands.Replace(t.Context(), target, created.Revision, changedMethod)
			if err != nil || !replaced.PreviousRemoved || replaced.Revision == created.Revision {
				t.Fatal("method-only rotation lost its effect/fence", replaced, err)
			}
			request(0, false)
			request(1, true)
			rotated, err := commands.Replace(t.Context(), target, replaced.Revision, changedPassword)
			if err != nil || rotated.Revision == replaced.Revision {
				t.Fatal("password rotation lost its fence", rotated, err)
			}
			request(1, false)
			request(2, true)
			request(3, true)
			if _, err := commands.Remove(t.Context(), target, old.Email, replaced.Revision); !errors.Is(err, users.ErrConflict) {
				t.Fatal("stale legacy removal admitted", err)
			}
			if _, err := commands.Remove(t.Context(), target, old.Email, rotated.Revision); err != nil {
				t.Fatal(err)
			}
			request(2, false)
			request(3, true)
			restarted, err := server.Restart(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			assertNativeShutdown(t, restarted.StopFailure)
			request(3, false)
			if _, err := commands.Create(t.Context(), target, old); !errors.Is(err, instance.ErrExecutionConflict) {
				t.Fatal("stale legacy generation admitted", err)
			}
		})
	}
}

func TestLegacyShadowsocksSamePasswordAcrossMethodsIsIndependent(t *testing.T) {
	server, port := startAccountServer(t, users.Shadowsocks)
	commands := nativeAccountCommands(t, server)
	target := users.Target{Generation: server.Status().Generation, Inbound: "accounts"}
	first := accountInput(users.Shadowsocks, "s", 1)
	first.Method = users.MethodAES256GCM
	second := first
	second.Email, second.Method = "sibling", users.MethodChaCha20Poly1305
	created, err := commands.Create(t.Context(), target, first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commands.Create(t.Context(), target, second); err != nil {
		t.Fatal(err)
	}
	reader, err := xray.NewUserReader(1<<20, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	queries, err := users.New(server, reader, 100)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := queries.Get(t.Context(), target, "ſ")
	if err != nil || observed.Email != first.Email || observed.Level != first.Level || observed.Revision != created.Revision {
		t.Fatal("legacy native identity/revision read lost", observed, err)
	}
	if count, err := queries.Count(t.Context(), target); err != nil || count != 2 {
		t.Fatal("legacy native count", count, err)
	}
	if list, err := queries.List(t.Context(), target, 2); err != nil || len(list) != 2 || list[0].Email != first.Email || list[1].Email != second.Email {
		t.Fatal("legacy native ordered list", list, err)
	}
	request := secureAccountProbe(t, nativeTLSCertificates(t), startAccountClients(t, port, []users.Account{first, second}))
	request(0, true)
	request(1, true)
	if _, err := commands.Remove(t.Context(), target, "ſ", created.Revision); err != nil {
		t.Fatal("native EqualFold removal failed", err)
	}
	request(0, false)
	request(1, true)
}

func TestLegacyShadowsocksNoneIsRejectedByNativeConfiguration(t *testing.T) {
	source := fmt.Appendf(nil, `{"inbounds":[{"listen":"127.0.0.1","port":%d,"protocol":"shadowsocks","settings":{"method":"none","password":"isolated-test-password"}}],"outbounds":[{"protocol":"freedom"}]}`, accountServerPort(t))
	life, _, _ := nativeInstance(t, source)
	snapshot, err := life.Start(t.Context())
	var rejected *xray.CheckError
	if !errors.As(err, &rejected) || !strings.Contains(string(rejected.Diagnostics), "unknown cipher method") || snapshot.AppliedRevision != "" || snapshot.Process.PID != 0 {
		t.Fatal("unsupported native method claimed an applied execution", err)
	}
}

func BenchmarkShadowsocksNativeInspection(b *testing.B) {
	for _, size := range []int{1, 100} {
		b.Run(fmt.Sprintf("users-%d", size), func(b *testing.B) {
			server, _ := startAccountServer(b, users.Shadowsocks)
			commands := nativeAccountCommands(b, server)
			target := users.Target{Generation: server.Status().Generation, Inbound: "accounts"}
			methods := []users.ShadowsocksMethod{users.MethodAES128GCM, users.MethodAES256GCM, users.MethodChaCha20Poly1305, users.MethodXChaCha20Poly1305}
			account := accountInput(users.Shadowsocks, "0", 1)
			for i := range size {
				input := accountInput(users.Shadowsocks, fmt.Sprint(i), byte(i+1))
				input.Method = methods[i%len(methods)]
				if _, err := commands.Create(b.Context(), target, input); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				if result, err := commands.Create(b.Context(), target, account); err != nil || result.Outcome != users.OutcomeUnchanged {
					b.Fatal("native legacy observation changed account", result, err)
				}
			}
		})
	}
}
