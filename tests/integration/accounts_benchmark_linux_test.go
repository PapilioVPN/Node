package integration_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/cores/xray"
	"github.com/papilio-platform/Node/internal/instance"
	"github.com/papilio-platform/Node/internal/users"
)

func BenchmarkAccountNativeMutations(b *testing.B) {
	for _, size := range []int{1, 100, 1000, 10000} {
		b.Run(fmt.Sprintf("users-%d", size), func(b *testing.B) {
			server, account := benchmarkAccountPopulation(b, size)
			adapter, err := xray.NewUserAccounts(xray.UserAccountOptions{
				ReplyBytes: 16 << 20, MaxUsers: size, MaxInbounds: 16, Timeout: 3 * time.Second,
			})
			if err != nil {
				b.Fatal(err)
			}
			commands, err := users.NewCommands(server, adapter, adapter, 3*time.Second)
			if err != nil {
				b.Fatal(err)
			}
			reader, err := xray.NewUserReader(16<<20, 3*time.Second)
			if err != nil {
				b.Fatal(err)
			}
			queries, err := users.New(server, reader, size)
			if err != nil {
				b.Fatal(err)
			}
			target := users.Target{Generation: server.Status().Generation, Inbound: "accounts"}
			if count, err := queries.Count(b.Context(), target); err != nil || count != int64(size) {
				b.Fatal("native fixture population", count, err)
			}
			observed, err := queries.Get(b.Context(), target, account.Email)
			if err != nil {
				b.Fatal(err)
			}
			revision := observed.Revision
			b.Run("unchanged", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					result, err := commands.Create(b.Context(), target, account)
					if err != nil || result.Outcome != users.OutcomeUnchanged || result.Revision != revision {
						b.Fatal("unchanged native account", err)
					}
				}
			})
			b.Run("replace", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					account.Level++
					result, err := commands.Replace(b.Context(), target, revision, account)
					if err != nil || !result.PreviousRemoved || result.Outcome != users.OutcomeApplied || result.Revision == revision {
						b.Fatal("native replacement effects", err)
					}
					revision = result.Revision
				}
			})
			final, err := queries.Get(b.Context(), target, account.Email)
			if err != nil || final.Revision != revision || final.Level != account.Level {
				b.Fatal("final applied native revision", err)
			}
			if count, err := queries.Count(b.Context(), target); err != nil || count != int64(size) {
				b.Fatal("native mutation changed population", count, err)
			}
		})
	}
}

// Populate through one native startup; repeated Create would make setup O(N²).
func benchmarkAccountPopulation(b *testing.B, size int) (*instance.Service, users.Account) {
	b.Helper()
	type client struct {
		Email    string `json:"email"`
		Level    uint32 `json:"level"`
		Password string `json:"password"`
	}
	clients := make([]client, size)
	for i := range clients {
		clients[i] = client{Email: fmt.Sprintf("measured-%d", i), Level: 1,
			Password: fmt.Sprintf("isolated-benchmark-password-%d", i)}
	}
	encoded, err := json.Marshal(clients)
	if err != nil {
		b.Fatal(err)
	}
	source := fmt.Appendf(nil, `{"log":{"loglevel":"none"},"inbounds":[{"tag":"accounts","listen":"127.0.0.1","port":%d,"protocol":"trojan","settings":{"clients":%s}}],"outbounds":[{"protocol":"blackhole"}]}`, accountServerPort(b), encoded)
	server, _, _ := nativeInstance(b, source)
	if _, err := server.Start(b.Context()); err != nil {
		b.Fatal(err)
	}
	return server, users.Account{Email: clients[0].Email, Level: 1,
		Protocol: users.Trojan, Password: clients[0].Password}
}
