package xray

import (
	"github.com/papilio-platform/Node/internal/users"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/proxy/shadowsocks"
	"google.golang.org/protobuf/proto"
)

func encodeShadowsocksAccount(account users.Account) (*shadowsocks.Account, error) {
	var cipher shadowsocks.CipherType
	switch account.Method {
	case users.MethodAES128GCM:
		cipher = shadowsocks.CipherType_AES_128_GCM
	case users.MethodAES256GCM:
		cipher = shadowsocks.CipherType_AES_256_GCM
	case users.MethodChaCha20Poly1305:
		cipher = shadowsocks.CipherType_CHACHA20_POLY1305
	case users.MethodXChaCha20Poly1305:
		cipher = shadowsocks.CipherType_XCHACHA20_POLY1305
	default:
		return nil, users.ErrUnsupported
	}
	return &shadowsocks.Account{Password: account.Password, CipherType: cipher}, nil
}

func shadowsocksAccountKey(user *protocol.User) (string, bool, error) {
	account := &shadowsocks.Account{}
	if user.Account.Type != serial.GetMessageType(account) || proto.Unmarshal(user.Account.Value, account) != nil {
		return "", false, users.ErrUnsupported
	}
	memory, err := account.AsAccount()
	if err != nil {
		return "", false, users.ErrUnsupported
	}
	// Native authentication tries each user's cipher with its derived key.
	// Equal passwords under different methods are not authentication aliases.
	key := append([]byte{byte(account.CipherType)}, memory.(*shadowsocks.MemoryAccount).Key...)
	return string(key), proto.Equal(account, &shadowsocks.Account{Password: account.Password, CipherType: account.CipherType}), nil
}
