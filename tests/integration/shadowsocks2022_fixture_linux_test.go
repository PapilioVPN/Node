package integration_test

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/papilio-platform/Node/internal/instance"
	ss2022 "github.com/xtls/xray-core/proxy/shadowsocks_2022"
)

func startSS2022Managed(t testing.TB, method string) (*instance.Service, int, string) {
	t.Helper()
	port := accountServerPort(t)
	source, key := ss2022Source(t, port, method, `,"clients":[]`)
	life, _, _ := nativeInstance(t, source)
	if _, err := life.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return life, port, key
}

func ss2022Source(t testing.TB, port int, method, membership string) ([]byte, string) {
	t.Helper()
	cipher, err := ss2022.GetCipherMethod(method)
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, cipher.KeySaltLength)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(raw)
	source := fmt.Appendf(nil, `{"log":{"loglevel":"none"},"inbounds":[{"tag":"accounts","listen":"127.0.0.1","port":%d,"protocol":"shadowsocks","settings":{"method":%q,"password":%q,"network":"tcp"%s}}],"outbounds":[{"protocol":"freedom","settings":{"finalRules":[{"action":"allow","ip":["127.0.0.1/32"]}]}}]}`, port, method, key, membership)
	return source, key
}

func startSS2022Clients(t *testing.T, port int, method string, passwords []string) []string {
	t.Helper()
	root, err := os.MkdirTemp("", "papilio-ss2022-client-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	var inbounds, outbounds, routes, sockets []string
	for i, password := range passwords {
		tag := fmt.Sprintf("client-%d", i)
		socket := filepath.Join(root, fmt.Sprintf("%d.sock", i))
		sockets = append(sockets, socket)
		inbounds = append(inbounds, fmt.Sprintf(`{"tag":%q,"listen":%q,"protocol":"http","settings":{}}`, tag, socket))
		outbounds = append(outbounds, fmt.Sprintf(`{"tag":%q,"protocol":"shadowsocks","settings":{"servers":[{"address":"127.0.0.1","port":%d,"method":%q,"password":%q}]},"mux":{"enabled":false}}`, tag, port, method, password))
		routes = append(routes, fmt.Sprintf(`{"type":"field","inboundTag":[%q],"outboundTag":%q}`, tag, tag))
	}
	source := fmt.Appendf(nil, `{"log":{"loglevel":"none"},"inbounds":[%s],"outbounds":[%s],"routing":{"rules":[%s]}}`, strings.Join(inbounds, ","), strings.Join(outbounds, ","), strings.Join(routes, ","))
	life, _, _ := nativeInstance(t, source)
	if _, err := life.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return sockets
}
