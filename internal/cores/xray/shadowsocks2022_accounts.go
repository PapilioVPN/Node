package xray

import (
	"github.com/papilio-platform/Node/internal/users"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	ss2022 "github.com/xtls/xray-core/proxy/shadowsocks_2022"
	"google.golang.org/protobuf/proto"
)

func shadowsocks2022AccountKey(user *protocol.User, keyBytes int) (string, bool, error) {
	account := &ss2022.Account{}
	if user.Account.Type != serial.GetMessageType(account) || proto.Unmarshal(user.Account.Value, account) != nil {
		return "", false, users.ErrUnsupported
	}
	key, err := ss2022.ParseKey(account.Key, keyBytes)
	if err != nil {
		return "", false, users.ErrUnsupported
	}
	hash := ss2022.DeriveUserPSKHash(key)
	return string(hash[:]), proto.Equal(account, &ss2022.Account{Key: account.Key}), nil
}
