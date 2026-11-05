package xray

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/papilio-platform/Node/internal/users"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/common/uuid"
	ss2022 "github.com/xtls/xray-core/proxy/shadowsocks_2022"
	"github.com/xtls/xray-core/proxy/trojan"
	"github.com/xtls/xray-core/proxy/vless"
	"github.com/xtls/xray-core/proxy/vmess"
	"google.golang.org/protobuf/proto"
)

type accountBinding struct {
	family   users.Protocol
	keyBytes int
}

func encodeAccount(account users.Account) (*protocol.User, error) {
	id := uuid.UUID(account.ID)
	var credential proto.Message
	switch account.Protocol {
	case users.VMess:
		credential = &vmess.Account{Id: id.String()}
	case users.VLESS:
		credential = &vless.Account{Id: id.String(), Flow: string(account.Flow)}
	case users.Trojan:
		credential = &trojan.Account{Password: account.Password}
	case users.Shadowsocks:
		var err error
		credential, err = encodeShadowsocksAccount(account)
		if err != nil {
			return nil, err
		}
	case users.Shadowsocks2022:
		credential = &ss2022.Account{Key: account.Key}
	default:
		return nil, users.ErrUnsupported
	}
	user := &protocol.User{Email: account.Email, Level: account.Level, Account: serial.ToTypedMessage(credential)}
	memory, err := user.ToMemoryUser()
	if err != nil {
		return nil, users.ErrInvalid
	}
	return protocol.ToProtoUser(memory), nil
}

// Keys follow native authentication indexes. The boolean permits replacement
// only when the account's entire representation is supported by our input.
func accountKey(user *protocol.User, binding accountBinding) (string, bool, error) {
	if user == nil || user.Account == nil {
		return "", false, users.ErrUnsupported
	}
	switch binding.family {
	case users.VMess:
		account := &vmess.Account{}
		if user.Account.Type != serial.GetMessageType(account) || proto.Unmarshal(user.Account.Value, account) != nil {
			return "", false, users.ErrUnsupported
		}
		id, err := uuid.ParseString(account.Id)
		if err != nil {
			return "", false, users.ErrUnsupported
		}
		return string(protocol.NewID(id).CmdKey()), true, nil
	case users.VLESS:
		account := &vless.Account{}
		if user.Account.Type != serial.GetMessageType(account) || proto.Unmarshal(user.Account.Value, account) != nil {
			return "", false, users.ErrUnsupported
		}
		id, err := uuid.ParseString(account.Id)
		if err != nil {
			return "", false, users.ErrUnsupported
		}
		key := vless.ProcessUUID(id)
		supportedFlow := account.Flow == "" || account.Flow == vless.XRV
		return string(key[:]), supportedFlow && proto.Equal(account, &vless.Account{Id: account.Id, Flow: account.Flow}), nil
	case users.Trojan:
		account := &trojan.Account{}
		if user.Account.Type != serial.GetMessageType(account) || proto.Unmarshal(user.Account.Value, account) != nil {
			return "", false, users.ErrUnsupported
		}
		key := sha256.Sum224([]byte(account.Password))
		return string(key[:]), true, nil
	case users.Shadowsocks:
		return shadowsocksAccountKey(user)
	case users.Shadowsocks2022:
		return shadowsocks2022AccountKey(user, binding.keyBytes)
	default:
		return "", false, users.ErrUnsupported
	}
}

// The per-session key prevents a metadata revision becoming a password oracle.
func fingerprintAccount(key [32]byte, user *protocol.User) (users.AccountRevision, error) {
	data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(user)
	if err != nil {
		return "", errors.New("invalid native account representation")
	}
	hash := hmac.New(sha256.New, key[:])
	hash.Write(data)
	return users.AccountRevision(hex.EncodeToString(hash.Sum(nil))), nil
}
