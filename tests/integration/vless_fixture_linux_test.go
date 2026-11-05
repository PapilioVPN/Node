package integration_test

import (
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/papilio-platform/Node/internal/instance"
	"github.com/papilio-platform/Node/internal/users"
)

func startVLESSServer(t *testing.T, material nativeTestTLS, version string, seed users.Account) (*instance.Service, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	// The static seed inherits the source default; live commands are explicit.
	source := fmt.Appendf(nil, `{"log":{"loglevel":"none"},"inbounds":[{"tag":"accounts","listen":"127.0.0.1","port":%d,"protocol":"vless","settings":{"decryption":"none","flow":"xtls-rprx-vision","clients":[{"email":%q,"id":%q}]},"streamSettings":{"network":"raw","security":"tls","tlsSettings":{"minVersion":%q,"maxVersion":%q,"certificates":[{"certificateFile":%q,"keyFile":%q}]}}}],"outbounds":[{"protocol":"freedom","settings":{"finalRules":[{"action":"allow","ip":["127.0.0.1/32"]}]}}]}`, port, seed.Email, hex.EncodeToString(seed.ID[:]), version, version, material.cert, material.key)
	life, _, _ := nativeInstance(t, source)
	if _, err := life.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return life, port
}

type vlessTestClient struct {
	account users.Account
	name    string
	root    string
	version string
}

func startVLESSClients(t *testing.T, port int, clients []vlessTestClient) []string {
	t.Helper()
	root, err := os.MkdirTemp("", "papilio-vless-client-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	var inbounds, outbounds, routes, sockets []string
	for i, client := range clients {
		tag := fmt.Sprintf("client-%d", i)
		socket := filepath.Join(root, fmt.Sprintf("%d.sock", i))
		sockets = append(sockets, socket)
		inbounds = append(inbounds, fmt.Sprintf(`{"tag":%q,"listen":%q,"protocol":"http","settings":{}}`, tag, socket))
		certs := "[]"
		if client.root != "" {
			certs = fmt.Sprintf(`[{"certificateFile":%q,"usage":"verify"}]`, client.root)
		}
		outbounds = append(outbounds, fmt.Sprintf(`{"tag":%q,"protocol":"vless","settings":{"vnext":[{"address":"127.0.0.1","port":%d,"users":[{"id":%q,"encryption":"none","flow":%q}]}]},"streamSettings":{"network":"raw","security":"tls","tlsSettings":{"serverName":%q,"minVersion":%q,"maxVersion":%q,"disableSystemRoot":true,"fingerprint":"unsafe","certificates":%s}},"mux":{"enabled":false}}`, tag, port, hex.EncodeToString(client.account.ID[:]), client.account.Flow, client.name, client.version, client.version, certs))
		routes = append(routes, fmt.Sprintf(`{"type":"field","inboundTag":[%q],"outboundTag":%q}`, tag, tag))
	}
	source := fmt.Appendf(nil, `{"log":{"loglevel":"none"},"inbounds":[%s],"outbounds":[%s],"routing":{"rules":[%s]}}`, strings.Join(inbounds, ","), strings.Join(outbounds, ","), strings.Join(routes, ","))
	life, _, _ := nativeInstance(t, source)
	if _, err := life.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return sockets
}
