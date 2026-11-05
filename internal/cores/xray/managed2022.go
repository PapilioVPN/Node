package xray

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	ss2022 "github.com/xtls/xray-core/proxy/shadowsocks_2022"
	"google.golang.org/protobuf/proto"
)

// ErrUnsupportedConfiguration rejects an unavailable managed execution mode.
var ErrUnsupportedConfiguration = errors.New("unsupported managed Xray configuration")

type empty2022Inbound struct {
	index int
	tag   string
}

type managed2022Selection struct {
	count   int
	empties []empty2022Inbound
}

func selectEmpty2022(source []byte) (managed2022Selection, error) {
	var document *struct {
		Inbounds []struct {
			Tag, Protocol string
			Settings      json.RawMessage
		}
	}
	if err := json.Unmarshal(source, &document); err != nil {
		return managed2022Selection{}, err
	}
	if document == nil {
		return managed2022Selection{}, errors.New("Xray configuration must be a JSON object")
	}
	selection := managed2022Selection{count: len(document.Inbounds)}
	for index, inbound := range document.Inbounds {
		if !strings.EqualFold(inbound.Protocol, "shadowsocks") {
			continue
		}
		var settings struct {
			Method         string
			Clients, Users []json.RawMessage
		}
		if err := json.Unmarshal(inbound.Settings, &settings); err != nil {
			return managed2022Selection{}, err
		}
		method, err := ss2022.GetCipherMethod(settings.Method)
		if err != nil {
			continue
		}
		members := settings.Users
		if settings.Clients != nil {
			members = settings.Clients
		}
		if members == nil || len(members) != 0 {
			continue
		}
		if method.IsChaCha {
			return managed2022Selection{}, ErrUnsupportedConfiguration
		}
		selection.empties = append(selection.empties, empty2022Inbound{index: index, tag: inbound.Tag})
	}
	return selection, nil
}

func rewriteEmpty2022(data []byte, selection managed2022Selection) ([]byte, error) {
	var config core.Config
	if err := proto.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	if len(config.Inbound) != selection.count {
		return nil, errors.New("compiled Xray inbound membership differs from source")
	}
	for _, target := range selection.empties {
		if target.index < 0 || target.index >= len(config.Inbound) {
			return nil, errors.New("invalid compiled Xray inbound index")
		}
		inbound := config.Inbound[target.index]
		if inbound == nil || inbound.Tag != target.tag || inbound.ProxySettings == nil || inbound.ProxySettings.Type != serial.GetMessageType(&ss2022.ServerConfig{}) {
			return nil, errors.New("compiled Xray inbound identity or mode differs from source")
		}
		var single ss2022.ServerConfig
		if err := proto.Unmarshal(inbound.ProxySettings.Value, &single); err != nil {
			return nil, err
		}
		method, err := ss2022.GetCipherMethod(single.Method)
		if err != nil || method.IsChaCha || len(single.ProtoReflect().GetUnknown()) != 0 {
			return nil, ErrUnsupportedConfiguration
		}
		inbound.ProxySettings = serial.ToTypedMessage(&ss2022.MultiUserServerConfig{Method: single.Method, Key: single.Key, Network: single.Network})
	}
	return proto.Marshal(&config)
}
