package xray

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	nativeNet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	ss2022 "github.com/xtls/xray-core/proxy/shadowsocks_2022"
	"google.golang.org/protobuf/proto"
)

func TestManaged2022SelectionPreservesNativeMembershipPresence(t *testing.T) {
	for _, test := range []struct {
		members string
		empty   bool
	}{
		{"", false},
		{`,"clients":null`, false},
		{`,"users":null`, false},
		{`,"clients":[]`, true},
		{`,"users":[]`, true},
		{`,"clients":null,"users":[]`, true},
		{`,"clients":[],"users":[{}]`, true},
		{`,"clients":[{}],"users":[]`, false},
		{`,"clients":[{}]`, false},
	} {
		t.Run(test.members, func(t *testing.T) {
			source := []byte(`{"inbounds":[{"tag":"owned","protocol":"shadowsocks","settings":{"method":"2022-blake3-aes-128-gcm"` + test.members + `}}]}`)
			before := bytes.Clone(source)
			selection, err := selectEmpty2022(source)
			if err != nil || (len(selection.empties) == 1) != test.empty || selection.count != 1 || !bytes.Equal(source, before) {
				t.Fatal("native presence/precedence or source ownership changed", selection, err)
			}
		})
	}
	for _, source := range []string{
		`{"inbounds":[{"protocol":"vmess","settings":{"clients":[]}}]}`,
		`{"inbounds":[{"protocol":"shadowsocks","settings":{"clients":[]}}]}`,
		`{"inbounds":[{"protocol":"shadowsocks","settings":{"method":"2022-blake3-chacha20-poly1305"}}]}`,
	} {
		if selection, err := selectEmpty2022([]byte(source)); err != nil || len(selection.empties) != 0 {
			t.Fatal("unrelated or native single-user input changed", err)
		}
	}
	if _, err := selectEmpty2022([]byte(`{"inbounds":[{"protocol":"shadowsocks","settings":{"method":"2022-blake3-chacha20-poly1305","clients":[]}}]}`)); !errors.Is(err, ErrUnsupportedConfiguration) {
		t.Fatal("unsupported managed method silently became single-user", err)
	}
}

func TestManaged2022RewritingPreservesOtherNativeConfiguration(t *testing.T) {
	original := &core.Config{
		App:      []*serial.TypedMessage{{Type: "opaque.app", Value: []byte("uninterpreted-native-settings")}},
		Outbound: []*core.OutboundHandlerConfig{{Tag: "direct", ProxySettings: &serial.TypedMessage{Type: "opaque.outbound", Value: []byte{6, 7, 8}}}},
		Inbound: []*core.InboundHandlerConfig{
			{Tag: "unrelated", ProxySettings: &serial.TypedMessage{Type: "opaque.proxy", Value: []byte{1, 2, 3}}},
			{Tag: "managed", ReceiverSettings: &serial.TypedMessage{Type: "opaque.receiver", Value: []byte{4, 5}}, ProxySettings: serial.ToTypedMessage(&ss2022.ServerConfig{Method: ss2022.MethodAES128GCM, Key: "isolated-test-key", Network: []nativeNet.Network{nativeNet.Network_TCP, nativeNet.Network_UDP}})},
		},
	}
	data, err := proto.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(data)
	selection := managed2022Selection{count: 2, empties: []empty2022Inbound{{index: 1, tag: "managed"}}}
	rewritten, err := rewriteEmpty2022(data, selection)
	if err != nil || !bytes.Equal(data, before) {
		t.Fatal("native preparation failed or mutated input", err)
	}
	expected := proto.Clone(original).(*core.Config)
	expected.Inbound[1].ProxySettings = serial.ToTypedMessage(&ss2022.MultiUserServerConfig{Method: ss2022.MethodAES128GCM, Key: "isolated-test-key", Network: []nativeNet.Network{nativeNet.Network_TCP, nativeNet.Network_UDP}})
	var got core.Config
	if err := proto.Unmarshal(rewritten, &got); err != nil || !proto.Equal(expected, &got) {
		t.Fatal("empty mode changed another native resource", err)
	}
	for _, bad := range []managed2022Selection{
		{count: 1, empties: selection.empties},
		{count: 2, empties: []empty2022Inbound{{index: 2}}},
		{count: 2, empties: []empty2022Inbound{{index: 0, tag: "unrelated"}}},
		{count: 2, empties: []empty2022Inbound{{index: 1, tag: "other"}}},
	} {
		if _, err := rewriteEmpty2022(data, bad); err == nil {
			t.Fatal("compiled input mismatch silently patched another inbound")
		}
	}
}

func FuzzManaged2022Preparation(f *testing.F) {
	config, err := proto.Marshal(&core.Config{Inbound: []*core.InboundHandlerConfig{{Tag: "owned", ProxySettings: serial.ToTypedMessage(&ss2022.ServerConfig{Method: ss2022.MethodAES256GCM, Key: "isolated-test-key"})}}})
	if err != nil {
		f.Fatal(err)
	}
	f.Add([]byte(`{"inbounds":[{"tag":"owned","protocol":"shadowsocks","settings":{"method":"2022-blake3-aes-256-gcm","clients":[]}}]}`), config)
	f.Add([]byte(`null`), []byte{0xff})
	f.Fuzz(func(t *testing.T, source, compiled []byte) {
		if len(source) > 65536 || len(compiled) > 65536 {
			return
		}
		before := bytes.Clone(source)
		selection, err := selectEmpty2022(source)
		if !bytes.Equal(before, source) {
			t.Fatal("source input changed")
		}
		if err != nil {
			return
		}
		before = bytes.Clone(compiled)
		output, err := rewriteEmpty2022(compiled, selection)
		if !bytes.Equal(before, compiled) {
			t.Fatal("compiled input changed")
		}
		if err != nil {
			return
		}
		var result core.Config
		if err := proto.Unmarshal(output, &result); err != nil {
			t.Fatal(err)
		}
		for _, target := range selection.empties {
			var accountMode ss2022.MultiUserServerConfig
			settings := result.Inbound[target.index].ProxySettings
			if settings.Type != serial.GetMessageType(&accountMode) || proto.Unmarshal(settings.Value, &accountMode) != nil || len(accountMode.Users) != 0 {
				t.Fatal("managed empty gained a native account or single-user mode")
			}
		}
		if !json.Valid(source) {
			t.Fatal("invalid source reached compiled preparation")
		}
	})
}
