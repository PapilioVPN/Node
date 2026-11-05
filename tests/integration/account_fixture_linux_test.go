package integration_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/cores/xray"
	"github.com/papilio-platform/Node/internal/instance"
	"github.com/papilio-platform/Node/internal/users"
)

func nativeAccountCommands(t testing.TB, life *instance.Service) *users.Commands {
	t.Helper()
	adapter, err := xray.NewUserAccounts(xray.UserAccountOptions{ReplyBytes: 1 << 20, MaxUsers: 100, MaxInbounds: 16, Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	commands, err := users.NewCommands(life, adapter, adapter, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return commands
}

func accountInput(family users.Protocol, email string, index byte) users.Account {
	a := users.Account{Email: email, Level: 1, Protocol: family, ID: [16]byte{6: 0x40, 8: 0x80, 15: index}}
	if family == users.Trojan {
		a.ID = [16]byte{}
		a.Password = fmt.Sprintf("isolated-test-password-%d", index)
	}
	return a
}

// reserve-close selects an ephemeral loopback port for the native listener.
// A competing bind fails startup rather than falling back to another service.
func startAccountServer(t testing.TB, family users.Protocol) (*instance.Service, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	kind := map[users.Protocol]string{users.VMess: "vmess", users.VLESS: "vless", users.Trojan: "trojan"}[family]
	settings := `{"clients":[]}`
	if family == users.VLESS {
		settings = `{"decryption":"none","clients":[]}`
	}
	source := fmt.Appendf(nil, `{"log":{"loglevel":"none"},"inbounds":[{"tag":"accounts","listen":"127.0.0.1","port":%d,"protocol":%q,"settings":%s}],"outbounds":[{"protocol":"freedom","settings":{"finalRules":[{"action":"allow","ip":["127.0.0.1/32"]}]}}]}`, port, kind, settings)
	life, _, _ := nativeInstance(t, source)
	if _, err := life.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return life, port
}

func startAccountClients(t testing.TB, port int, accounts []users.Account) []string {
	t.Helper()
	root, err := os.MkdirTemp("", "papilio-account-client-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	var inbounds, outbounds, routes, sockets []string
	for i, account := range accounts {
		tag := fmt.Sprintf("client-%d", i)
		socket := filepath.Join(root, fmt.Sprintf("%d.sock", i))
		sockets = append(sockets, socket)
		inbounds = append(inbounds, fmt.Sprintf(`{"tag":%q,"listen":%q,"protocol":"http","settings":{}}`, tag, socket))
		id := hex.EncodeToString(account.ID[:])
		kind := ""
		settings := ""
		switch account.Protocol {
		case users.VMess:
			kind = "vmess"
			settings = fmt.Sprintf(`{"vnext":[{"address":"127.0.0.1","port":%d,"users":[{"id":%q,"security":"auto"}]}]}`, port, id)
		case users.VLESS:
			kind = "vless"
			settings = fmt.Sprintf(`{"vnext":[{"address":"127.0.0.1","port":%d,"users":[{"id":%q,"encryption":"none","flow":%q}]}]}`, port, id, account.Flow)
		case users.Trojan:
			kind = "trojan"
			settings = fmt.Sprintf(`{"servers":[{"address":"127.0.0.1","port":%d,"password":%q}]}`, port, account.Password)
		}
		outbounds = append(outbounds, fmt.Sprintf(`{"tag":%q,"protocol":%q,"settings":%s,"mux":{"enabled":false}}`, tag, kind, settings))
		routes = append(routes, fmt.Sprintf(`{"type":"field","inboundTag":[%q],"outboundTag":%q}`, tag, tag))
	}
	source := fmt.Appendf(nil, `{"log":{"loglevel":"none"},"inbounds":[%s],"outbounds":[%s],"routing":{"rules":[%s]}}`, strings.Join(inbounds, ","), strings.Join(outbounds, ","), strings.Join(routes, ","))
	life, _, _ := nativeInstance(t, source)
	if _, err := life.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return sockets
}

func accountHTTPClient(t testing.TB, socket string) *http.Client {
	t.Helper()
	transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "owned-client.invalid:8080"}), DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: time.Second}
}
