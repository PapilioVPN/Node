package xray

import (
	"context"
	"errors"

	"github.com/papilio-platform/Node/internal/users"
	"github.com/xtls/xray-core/app/proxyman/command"
	"github.com/xtls/xray-core/common/protocol"
	ss2022 "github.com/xtls/xray-core/proxy/shadowsocks_2022"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type nativeAccount struct {
	user        *protocol.User
	key         string
	replaceable bool
}

// The native API has no focused inbound-type or credential-index query. Bounds
// apply to its whole responses; account records never escape this adapter.
func (a *UserAccounts) readBinding(ctx context.Context, client command.HandlerServiceClient, inbound string) (accountBinding, error) {
	if err := ctx.Err(); err != nil {
		return accountBinding{}, err
	}
	response, err := client.ListInbounds(ctx, &command.ListInboundsRequest{}, grpc.MaxCallRecvMsgSize(a.options.ReplyBytes))
	if err != nil {
		return accountBinding{}, userQueryError(ctx, err)
	}
	if response == nil {
		return accountBinding{}, errors.New("missing Xray inbound response")
	}
	if len(response.Inbounds) > a.options.MaxInbounds {
		return accountBinding{}, users.ErrLimit
	}
	var binding accountBinding
	for _, item := range response.Inbounds {
		if item == nil || item.Tag != inbound || item.ProxySettings == nil {
			continue
		}
		switch item.ProxySettings.Type {
		case "xray.proxy.vmess.inbound.Config":
			binding.family = users.VMess
		case "xray.proxy.vless.inbound.Config":
			binding.family = users.VLESS
		case "xray.proxy.trojan.ServerConfig":
			binding.family = users.Trojan
		case "xray.proxy.shadowsocks.ServerConfig":
			binding.family = users.Shadowsocks
		case "xray.proxy.shadowsocks_2022.MultiUserServerConfig":
			var settings ss2022.MultiUserServerConfig
			if err := proto.Unmarshal(item.ProxySettings.Value, &settings); err != nil {
				return accountBinding{}, users.ErrUnsupported
			}
			method, err := ss2022.GetCipherMethod(settings.Method)
			if err != nil || method.IsChaCha {
				return accountBinding{}, users.ErrUnsupported
			}
			binding.family, binding.keyBytes = users.Shadowsocks2022, method.KeySaltLength
		default:
			return accountBinding{}, users.ErrUnsupported
		}
		break
	}
	if binding.family == 0 {
		return accountBinding{}, users.ErrNotFound
	}
	return binding, nil
}

func (a *UserAccounts) readAccounts(ctx context.Context, client command.HandlerServiceClient, inbound string, binding accountBinding) ([]nativeAccount, error) {
	count, err := readUserCount(ctx, client, inbound, a.options.ReplyBytes)
	if err != nil {
		return nil, err
	}
	if count > int64(a.options.MaxUsers) {
		return nil, users.ErrLimit
	}
	collection, err := client.GetInboundUsers(ctx, &command.GetInboundUserRequest{Tag: inbound}, grpc.MaxCallRecvMsgSize(a.options.ReplyBytes))
	if err != nil {
		return nil, userQueryError(ctx, err)
	}
	if collection == nil {
		return nil, errors.New("missing Xray account collection")
	}
	if len(collection.Users) > a.options.MaxUsers {
		return nil, users.ErrLimit
	}
	records := make([]nativeAccount, 0, len(collection.Users))
	for _, user := range collection.Users {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key, replaceable, err := accountKey(user, binding)
		if err != nil {
			return nil, err
		}
		records = append(records, nativeAccount{user: user, key: key, replaceable: replaceable})
	}
	if int64(len(records)) != count {
		return nil, users.ErrConflict
	}
	return records, nil
}
