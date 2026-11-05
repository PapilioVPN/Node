package xray

import (
	"context"
	"errors"

	"github.com/papilio-platform/Node/internal/instance"
	"github.com/papilio-platform/Node/internal/users"
	"github.com/xtls/xray-core/app/proxyman/command"
	"github.com/xtls/xray-core/common/protocol"
	"google.golang.org/grpc"
)

type nativeAccount struct {
	user  *protocol.User
	key   string
	basic bool
}

// The native API has no focused inbound-type or credential-index query. Bounds
// apply to its whole responses; account records never escape this adapter.
func (a *UserAccounts) observeAccounts(ctx context.Context, session instance.ISession, inbound string) (users.Protocol, []nativeAccount, error) {
	api, err := userAPIFor(session)
	if err != nil {
		return 0, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, a.options.Timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	response, err := api.handlers.ListInbounds(ctx, &command.ListInboundsRequest{}, grpc.MaxCallRecvMsgSize(a.options.ReplyBytes))
	if err != nil {
		return 0, nil, userQueryError(ctx, err)
	}
	if response == nil {
		return 0, nil, errors.New("missing Xray inbound response")
	}
	if len(response.Inbounds) > a.options.MaxInbounds {
		return 0, nil, users.ErrLimit
	}
	var family users.Protocol
	for _, item := range response.Inbounds {
		if item == nil || item.Tag != inbound || item.ProxySettings == nil {
			continue
		}
		switch item.ProxySettings.Type {
		case "xray.proxy.vmess.inbound.Config":
			family = users.VMess
		case "xray.proxy.vless.inbound.Config":
			family = users.VLESS
		case "xray.proxy.trojan.ServerConfig":
			family = users.Trojan
		default:
			return 0, nil, users.ErrUnsupported
		}
		break
	}
	if family == 0 {
		return 0, nil, users.ErrNotFound
	}
	count, err := readUserCount(ctx, api.handlers, inbound, a.options.ReplyBytes)
	if err != nil {
		return 0, nil, err
	}
	if count > int64(a.options.MaxUsers) {
		return 0, nil, users.ErrLimit
	}
	collection, err := api.handlers.GetInboundUsers(ctx, &command.GetInboundUserRequest{Tag: inbound}, grpc.MaxCallRecvMsgSize(a.options.ReplyBytes))
	if err != nil {
		return 0, nil, userQueryError(ctx, err)
	}
	if collection == nil {
		return 0, nil, errors.New("missing Xray account collection")
	}
	if len(collection.Users) > a.options.MaxUsers {
		return 0, nil, users.ErrLimit
	}
	records := make([]nativeAccount, 0, len(collection.Users))
	for _, user := range collection.Users {
		if err := ctx.Err(); err != nil {
			return 0, nil, err
		}
		key, basic, err := accountKey(user, family)
		if err != nil {
			return 0, nil, err
		}
		records = append(records, nativeAccount{user: user, key: key, basic: basic})
	}
	return family, records, nil
}
