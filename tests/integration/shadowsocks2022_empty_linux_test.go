package integration_test

import (
	"errors"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/cores/xray"
	"github.com/papilio-platform/Node/internal/users"
)

func TestSS2022ExplicitEmptyStartsWithoutSyntheticUsersOrServerKeyAccess(t *testing.T) {
	for _, method := range []string{"2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm"} {
		t.Run(method, func(t *testing.T) {
			port := accountServerPort(t)
			source, key := ss2022Source(t, port, method, `,"clients":[]`)
			life, _, _ := nativeInstance(t, source)
			if _, err := life.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			reader, err := xray.NewUserReader(1<<20, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			queries, err := users.New(life, reader, 100)
			if err != nil {
				t.Fatal(err)
			}
			target := users.Target{Generation: life.Status().Generation, Inbound: "accounts"}
			if count, err := queries.Count(t.Context(), target); err != nil || count != 0 {
				t.Error("explicit managed empty was not a real zero-user inbound", count, err)
			}
			if list, err := queries.List(t.Context(), target, 100); err != nil || len(list) != 0 {
				t.Error("empty managed inbound contains synthetic users or lacks native membership", list, err)
			}
			if _, err := queries.Get(t.Context(), target, "missing"); !errors.Is(err, users.ErrNotFound) {
				t.Error("native empty identity was not absent", err)
			}
			sockets := startSS2022Clients(t, port, method, []string{key})
			request := secureAccountProbe(t, nativeTLSCertificates(t), sockets)
			request(0, false)
			restarted, err := life.Restart(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			assertNativeShutdown(t, restarted.StopFailure)
			target.Generation = restarted.Current.Generation
			if count, err := queries.Count(t.Context(), target); err != nil || count != 0 {
				t.Fatal("restart lost empty native membership", count, err)
			}
			request(0, false)
		})
	}
}

func TestSS2022OmittedMembershipPreservesNativeSingleUser(t *testing.T) {
	port := accountServerPort(t)
	source, key := ss2022Source(t, port, "2022-blake3-aes-128-gcm", "")
	life, _, _ := nativeInstance(t, source)
	if _, err := life.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	sockets := startSS2022Clients(t, port, "2022-blake3-aes-128-gcm", []string{key})
	request := secureAccountProbe(t, nativeTLSCertificates(t), sockets)
	request(0, true)
}
