package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/cores/xray"
	"github.com/papilio-platform/Node/internal/instance"
	"github.com/papilio-platform/Node/internal/users"
)

func nativeUsers(t testing.TB) (*users.Service, *instance.Service) {
	t.Helper()
	root, err := os.MkdirTemp("", "papilio-users-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	source := fmt.Appendf(nil, `{"log":{"loglevel":"none"},"inbounds":[
{"tag":"vmess","listen":%q,"protocol":"vmess","settings":{"clients":[{"id":"00000000-0000-4000-8000-000000000001","email":"zeta","level":2},{"id":"00000000-0000-4000-8000-000000000002","email":"alpha","level":1}]}},
{"tag":"vless","listen":%q,"protocol":"vless","settings":{"decryption":"none","clients":[{"id":"00000000-0000-4000-8000-000000000003","email":"vless-user","level":3}]}},
{"tag":"trojan","listen":%q,"protocol":"trojan","settings":{"clients":[{"password":"isolated-test-password","email":"trojan-user","level":4}]}},
{"tag":"empty","listen":%q,"protocol":"vmess","settings":{"clients":[]}},
{"tag":"http","listen":%q,"protocol":"http","settings":{}}
],"outbounds":[{"protocol":"freedom"}]}`, filepath.Join(root, "m.sock"), filepath.Join(root, "l.sock"), filepath.Join(root, "t.sock"), filepath.Join(root, "e.sock"), filepath.Join(root, "h.sock"))
	life, _, _ := nativeInstance(t, source)
	reader, err := xray.NewUserReader(1<<20, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	service, err := users.New(life, reader, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := life.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return service, life
}

func TestUsersReadActualAccountsAndFenceRestart(t *testing.T) {
	s, life := nativeUsers(t)
	initial := life.Status()
	for _, test := range []struct {
		tag, email string
		level      uint32
		count      int64
	}{{"vmess", "alpha", 1, 2}, {"vless", "vless-user", 3, 1}, {"trojan", "trojan-user", 4, 1}} {
		target := users.Target{Generation: initial.Generation, Inbound: test.tag}
		u, err := s.Get(t.Context(), target, test.email)
		if err != nil || u.Email != test.email || u.Level != test.level {
			t.Fatalf("native %s read: %+v %v", test.tag, u, err)
		}
		count, err := s.Count(t.Context(), target)
		if err != nil || count != test.count {
			t.Fatalf("native %s count: %d %v", test.tag, count, err)
		}
		list, err := s.List(t.Context(), target, int(test.count))
		if err != nil || len(list) != int(test.count) {
			t.Fatalf("native %s list: %+v %v", test.tag, list, err)
		}
	}
	target := users.Target{Generation: initial.Generation, Inbound: "vmess"}
	list, err := s.List(t.Context(), target, 2)
	if err != nil || list[0].Email != "alpha" || list[1].Email != "zeta" {
		t.Fatalf("native collection order: %+v %v", list, err)
	}
	if _, err := s.Get(t.Context(), target, "missing"); !errors.Is(err, users.ErrNotFound) {
		t.Fatal("native missing user", err)
	}
	if _, err := s.List(t.Context(), target, 1); !errors.Is(err, users.ErrLimit) {
		t.Fatal("native count limit", err)
	}
	target.Inbound = "empty"
	empty, err := s.List(t.Context(), target, 1)
	if err != nil || len(empty) != 0 {
		t.Fatalf("native empty collection: %+v %v", empty, err)
	}
	result, err := life.Restart(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	assertNativeShutdown(t, result.StopFailure)
	if _, err := s.Count(t.Context(), target); !errors.Is(err, instance.ErrExecutionConflict) {
		t.Fatal("stale query reached replacement", err)
	}
	target.Generation = result.Current.Generation
	target.Inbound = "trojan"
	if u, err := s.Get(t.Context(), target, "trojan-user"); err != nil || u.Level != 4 {
		t.Fatal("new execution read failed", err)
	}
}

func TestUsersNativeResponseByteLimitAndEngineFailures(t *testing.T) {
	s, life := nativeUsers(t)
	target := users.Target{Generation: life.Status().Generation, Inbound: "vmess"}
	reader, err := xray.NewUserReader(32, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	bounded, err := users.New(life, reader, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := bounded.Count(t.Context(), target); err != nil || n != 2 {
		t.Fatal("small count reply did not fit", err)
	}
	if _, err := bounded.Get(t.Context(), target, "alpha"); !errors.Is(err, users.ErrLimit) {
		t.Fatal("real gRPC byte limit not enforced", err)
	}
	if _, err := bounded.List(t.Context(), target, 2); !errors.Is(err, users.ErrLimit) {
		t.Fatal("real collection byte limit not enforced", err)
	}
	target.Inbound = "http"
	if _, err := s.Count(t.Context(), target); err == nil {
		t.Fatal("non-user inbound fabricated count")
	}
	target.Inbound = "missing"
	if _, err := s.Count(t.Context(), target); err == nil {
		t.Fatal("missing inbound fabricated zero")
	}
	if life.Status().Phase != instance.PhaseReady {
		t.Fatal("read errors changed healthy lifecycle")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Get(ctx, target, "alpha"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled native read", err)
	}
}

func BenchmarkUsersNativeQueries(b *testing.B) {
	s, life := nativeUsers(b)
	target := users.Target{Generation: life.Status().Generation, Inbound: "vmess"}
	b.Run("single", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := s.Get(b.Context(), target, "alpha"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("count", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if n, err := s.Count(b.Context(), target); err != nil || n != 2 {
				b.Fatal("count", err)
			}
		}
	})
	b.Run("list-two", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if list, err := s.List(b.Context(), target, 2); err != nil || len(list) != 2 {
				b.Fatal("list", err)
			}
		}
	})
}
