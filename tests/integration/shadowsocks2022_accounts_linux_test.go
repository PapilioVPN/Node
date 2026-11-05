package integration_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/cores/xray"
	"github.com/papilio-platform/Node/internal/instance"
	"github.com/papilio-platform/Node/internal/users"
)

func TestSS2022AccountMutationsControlActualTraffic(t *testing.T) {
	for _, method := range []string{"2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm"} {
		t.Run(method, func(t *testing.T) {
			server, port, master := startSS2022Managed(t, method)
			commands := nativeAccountCommands(t, server)
			queries := nativeUserQueries(t, server)
			target := users.Target{Generation: server.Status().Generation, Inbound: "accounts"}
			bytes := 16
			if strings.Contains(method, "256") {
				bytes = 32
			}
			old := users.Account{Email: "primary", Level: 1, Protocol: users.Shadowsocks2022, Key: base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", bytes)))}
			material := nativeTLSCertificates(t)
			requestOld := secureAccountProbe(t, material, startSS2022Clients(t, port, method, []string{master + ":" + old.Key, master}))
			requestOld(0, false)
			requestOld(1, false)
			created, err := commands.Create(t.Context(), target, old)
			if err != nil || created.Key != old.Key || created.Outcome != users.OutcomeApplied {
				t.Fatal("native PSK create", err)
			}
			requestOld(0, true)
			convertedReplay := old
			convertedReplay.Key, convertedReplay.Password = "", old.Key
			if replay, err := commands.Create(t.Context(), target, convertedReplay); err != nil || replay.Outcome != users.OutcomeUnchanged || replay.Key != old.Key || replay.Revision != created.Revision {
				t.Fatal("PSK/password replay changed credentials", err)
			}
			alias := old
			alias.Email = "alias"
			if _, err := commands.Create(t.Context(), target, alias); !errors.Is(err, users.ErrConflict) {
				t.Fatal("SS2022 native key-hash alias accepted", err)
			}
			sibling := users.Account{Email: "ſ", Level: 1, Protocol: users.Shadowsocks2022, Password: "isolated-sibling-password"}
			added, err := commands.Create(t.Context(), target, sibling)
			if err != nil || added.Key == "" {
				t.Fatal("password conversion did not return usable PSK", err)
			}
			requestSibling := secureAccountProbe(t, material, startSS2022Clients(t, port, method, []string{master + ":" + added.Key}))
			requestSibling(0, true)
			if count, err := queries.Count(t.Context(), target); err != nil || count != 2 {
				t.Fatal("native SS2022 membership count", count, err)
			}
			if list, err := queries.List(t.Context(), target, 2); err != nil || len(list) != 2 || list[0].Email != old.Email || list[1].Email != sibling.Email {
				t.Fatal("native SS2022 ordered metadata", err)
			}
			next := old
			next.Key, next.Password = "", "isolated-replacement-password"
			replaced, err := commands.Replace(t.Context(), target, created.Revision, next)
			if err != nil || !replaced.PreviousRemoved || replaced.Revision == created.Revision || replaced.Key == "" || replaced.Key == old.Key {
				t.Fatal("native PSK replacement", err)
			}
			requestNew := secureAccountProbe(t, material, startSS2022Clients(t, port, method, []string{master + ":" + replaced.Key}))
			requestOld(0, false)
			requestNew(0, true)
			requestSibling(0, true)
			observed, err := queries.Get(t.Context(), target, old.Email)
			if err != nil || observed.Revision != replaced.Revision {
				t.Fatal("native SS2022 account revision", err)
			}
			if _, err := commands.Remove(t.Context(), target, old.Email, created.Revision); !errors.Is(err, users.ErrConflict) {
				t.Fatal("stale PSK removal admitted", err)
			}
			if replay, err := commands.Replace(t.Context(), target, created.Revision, next); err != nil || replay.Outcome != users.OutcomeUnchanged || replay.Key != replaced.Key {
				t.Fatal("SS2022 replacement replay changed user", err)
			}
			if _, err := commands.Remove(t.Context(), target, old.Email, replaced.Revision); err != nil {
				t.Fatal(err)
			}
			requestNew(0, false)
			requestSibling(0, true)
			restarted, err := server.Restart(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			assertNativeShutdown(t, restarted.StopFailure)
			requestSibling(0, false)
			requestOld(1, false)
			if _, err := commands.Create(t.Context(), target, old); !errors.Is(err, instance.ErrExecutionConflict) {
				t.Fatal("stale SS2022 generation admitted", err)
			}
		})
	}
}

func TestSS2022UnindexedNativeUserFailsMutationsAndRestartClearsIt(t *testing.T) {
	server, _, _ := startSS2022Managed(t, "2022-blake3-aes-128-gcm")
	adapter, err := xray.NewUserAccounts(xray.UserAccountOptions{ReplyBytes: 1 << 20, MaxUsers: 100, MaxInbounds: 16, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	access, err := server.Acquire(t.Context(), server.Status().Generation)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a separate privileged native client, outside Node's input contract.
	err = adapter.Add(access.Context(), access.Session(), "accounts", users.Account{Protocol: users.Shadowsocks2022, Key: "MDEyMzQ1Njc4OWFiY2RlZg=="})
	access.Close()
	if err != nil {
		t.Fatal(err)
	}
	commands := nativeAccountCommands(t, server)
	queries := nativeUserQueries(t, server)
	target := users.Target{Generation: server.Status().Generation, Inbound: "accounts"}
	if count, err := queries.Count(t.Context(), target); err != nil || count != 1 {
		t.Fatal("native unnamed membership not observed", count, err)
	}
	account := users.Account{Email: "managed", Protocol: users.Shadowsocks2022, Password: "isolated-password"}
	if result, err := commands.Create(t.Context(), target, account); !errors.Is(err, users.ErrConflict) || result.Key != "" || result.Outcome != users.OutcomeUnchanged {
		t.Fatal("unindexed native membership became an authorized empty state", err)
	}
	if _, err := server.Restart(t.Context()); err != nil {
		t.Fatal(err)
	}
	target.Generation = server.Status().Generation
	if _, err := commands.Create(t.Context(), target, account); err != nil {
		t.Fatal("restart did not clear unindexed native state", err)
	}
}

func TestSS2022RemovalPreservesEstablishedStream(t *testing.T) {
	server, port, master := startSS2022Managed(t, "2022-blake3-aes-128-gcm")
	commands := nativeAccountCommands(t, server)
	target := users.Target{Generation: server.Status().Generation, Inbound: "accounts"}
	account := users.Account{Email: "stream", Protocol: users.Shadowsocks2022, Password: "isolated-stream-password"}
	created, err := commands.Create(t.Context(), target, account)
	if err != nil {
		t.Fatal(err)
	}
	sockets := startSS2022Clients(t, port, "2022-blake3-aes-128-gcm", []string{master + ":" + created.Key})
	verifyAccountStreamSurvives(t, sockets[0], func() {
		if _, err := commands.Remove(t.Context(), target, account.Email, created.Revision); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSS2022PasswordReplayAfterActualLostAcknowledgement(t *testing.T) {
	server, port, master := startSS2022Managed(t, "2022-blake3-aes-128-gcm")
	adapter, err := xray.NewUserAccounts(xray.UserAccountOptions{ReplyBytes: 1 << 20, MaxUsers: 100, MaxInbounds: 16, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	fault := &lostAccountAck{writer: adapter, loseAdd: true}
	commands, err := users.NewCommands(server, adapter, fault, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	target := users.Target{Generation: server.Status().Generation, Inbound: "accounts"}
	account := users.Account{Email: "replay", Protocol: users.Shadowsocks2022, Password: "isolated-replay-password"}
	if result, err := commands.Create(t.Context(), target, account); !errors.Is(err, errLostAccountAck) || result.Outcome != users.OutcomeUnknown || result.Key != "" {
		t.Fatal("unconfirmed key was exposed as applied", err)
	}
	replay, err := commands.Create(t.Context(), target, account)
	if err != nil || replay.Outcome != users.OutcomeUnchanged || replay.Key == "" {
		t.Fatal("settled native replay lost credential", err)
	}
	request := secureAccountProbe(t, nativeTLSCertificates(t), startSS2022Clients(t, port, "2022-blake3-aes-128-gcm", []string{master + ":" + replay.Key}))
	request(0, true)
}

func TestSS2022ChaChaSingleUserUsesFullConfigurationKeyReplacement(t *testing.T) {
	const method = "2022-blake3-chacha20-poly1305"
	port := accountServerPort(t)
	source, master := ss2022Source(t, port, method, "")
	settings, server, _ := nativeConfiguration(t, source)
	started, err := server.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	requestOld := secureAccountProbe(t, nativeTLSCertificates(t), startSS2022Clients(t, port, method, []string{master}))
	requestOld(0, true)
	commands := nativeAccountCommands(t, server)
	target := users.Target{Generation: started.Generation, Inbound: "accounts"}
	if _, err := commands.Create(t.Context(), target, users.Account{Email: "unsupported", Protocol: users.Shadowsocks2022, Password: "isolated-password"}); !errors.Is(err, users.ErrUnsupported) {
		t.Fatal("single-user engine advertised live multi-user commands", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	nextKey := base64.StdEncoding.EncodeToString(raw)
	nextSource := bytes.Replace(source, []byte(master), []byte(nextKey), 1)
	replaced, err := settings.Apply(t.Context(), started.DesiredRevision, nextSource)
	if err != nil {
		t.Fatal(err)
	}
	assertNativeShutdown(t, replaced.StopFailure)
	requestOld(0, false)
	requestNew := secureAccountProbe(t, nativeTLSCertificates(t), startSS2022Clients(t, port, method, []string{nextKey}))
	requestNew(0, true)
}

func BenchmarkSS2022NativeAccountInspection(b *testing.B) {
	for _, size := range []int{1, 100} {
		b.Run(fmt.Sprintf("users-%d", size), func(b *testing.B) {
			server, _, _ := startSS2022Managed(b, "2022-blake3-aes-128-gcm")
			commands := nativeAccountCommands(b, server)
			target := users.Target{Generation: server.Status().Generation, Inbound: "accounts"}
			account := users.Account{Email: "measured", Protocol: users.Shadowsocks2022, Password: "isolated-measured-password"}
			created, err := commands.Create(b.Context(), target, account)
			if err != nil {
				b.Fatal(err)
			}
			for i := 1; i < size; i++ {
				key := [16]byte{15: byte(i)}
				if _, err := commands.Create(b.Context(), target, users.Account{Email: fmt.Sprint(i), Protocol: users.Shadowsocks2022, Key: base64.StdEncoding.EncodeToString(key[:])}); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				if result, err := commands.Create(b.Context(), target, account); err != nil || result.Outcome != users.OutcomeUnchanged || result.Key != created.Key {
					b.Fatal("SS2022 repeated inspection changed credential", err)
				}
			}
		})
	}
}
