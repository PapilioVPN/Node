package xray

import (
	"context"
	"strings"
	"time"

	"github.com/papilio-platform/Node/internal/instance"
	"github.com/papilio-platform/Node/internal/users"
	"google.golang.org/protobuf/proto"
)

// UserAccountOptions bounds native account inspection and individual RPCs.
type UserAccountOptions struct {
	ReplyBytes  int
	MaxUsers    int
	MaxInbounds int
	Timeout     time.Duration
}

// UserAccounts checks and mutates native accounts through a borrowed session.
type UserAccounts struct{ options UserAccountOptions }

// NewUserAccounts requires explicit byte, collection and timeout bounds.
func NewUserAccounts(options UserAccountOptions) (*UserAccounts, error) {
	if options.ReplyBytes < 1 || options.ReplyBytes > 64<<20 || options.MaxUsers < 1 || options.MaxUsers > users.MaxListUsers || options.MaxInbounds < 1 || options.MaxInbounds > 10000 || options.Timeout <= 0 {
		return nil, users.ErrInvalid
	}
	return &UserAccounts{options: options}, nil
}

// Check verifies inbound type, exact account equality and authentication aliases.
func (a *UserAccounts) Check(ctx context.Context, session instance.ISession, inbound string, account users.Account) (users.AccountCheck, error) {
	ctx, cancel := context.WithTimeout(ctx, a.options.Timeout)
	defer cancel()
	api, err := userAPIFor(session)
	if err != nil {
		return users.AccountCheck{}, err
	}
	binding, err := a.readBinding(ctx, api.handlers, inbound)
	if err != nil {
		return users.AccountCheck{}, err
	}
	if binding.family != account.Protocol {
		return users.AccountCheck{}, users.ErrUnsupported
	}
	if binding.family == users.Shadowsocks2022 {
		account, err = users.PrepareShadowsocks2022(account, binding.keyBytes)
		if err != nil {
			return users.AccountCheck{}, err
		}
	}
	desired, err := encodeAccount(account)
	if err != nil {
		return users.AccountCheck{}, err
	}
	key, _, err := accountKey(desired, binding)
	if err != nil {
		return users.AccountCheck{}, err
	}
	records, err := a.readAccounts(ctx, api.handlers, inbound, binding)
	if err != nil {
		return users.AccountCheck{}, err
	}
	desiredRevision, err := fingerprintAccount(api.revisionKey, desired)
	if err != nil {
		return users.AccountCheck{}, err
	}
	current, err := selectAccount(records, account.Email, key, binding.family)
	if err != nil {
		return users.AccountCheck{}, err
	}
	if current == nil {
		return users.AccountCheck{Prepared: account, DesiredRevision: desiredRevision, AtCapacity: len(records) >= a.options.MaxUsers}, nil
	}
	if !current.replaceable {
		return users.AccountCheck{}, users.ErrUnsupported
	}
	currentRevision, err := fingerprintAccount(api.revisionKey, current.user)
	if err != nil {
		return users.AccountCheck{}, err
	}
	return users.AccountCheck{Prepared: account, Exists: true, Revision: currentRevision, DesiredRevision: desiredRevision, Equal: current.user.Level == desired.Level && proto.Equal(current.user.Account, desired.Account)}, nil
}

// CheckRemoval rejects ambiguous identity/credential aliases before deletion.
func (a *UserAccounts) CheckRemoval(ctx context.Context, session instance.ISession, inbound, email string) (users.RemovalCheck, error) {
	ctx, cancel := context.WithTimeout(ctx, a.options.Timeout)
	defer cancel()
	api, err := userAPIFor(session)
	if err != nil {
		return users.RemovalCheck{}, err
	}
	binding, err := a.readBinding(ctx, api.handlers, inbound)
	if err != nil {
		return users.RemovalCheck{}, err
	}
	records, err := a.readAccounts(ctx, api.handlers, inbound, binding)
	if err != nil {
		return users.RemovalCheck{}, err
	}
	current, err := selectAccount(records, email, "", binding.family)
	if err != nil {
		return users.RemovalCheck{}, err
	}
	result := users.RemovalCheck{}
	if current != nil {
		result.Exists = true
		revision, err := fingerprintAccount(api.revisionKey, current.user)
		if err != nil {
			return users.RemovalCheck{}, err
		}
		result.Revision = revision
	}
	return result, nil
}

func selectAccount(records []nativeAccount, email, desiredKey string, family users.Protocol) (*nativeAccount, error) {
	var current *nativeAccount
	identity := strings.ToLower(email)
	for i := range records {
		if strings.ToLower(records[i].user.Email) == identity || family == users.Shadowsocks && strings.EqualFold(records[i].user.Email, email) {
			if current != nil {
				return nil, users.ErrConflict
			}
			current = &records[i]
		}
	}
	for i := range records {
		record := &records[i]
		if record == current {
			continue
		}
		if desiredKey != "" && record.key == desiredKey || current != nil && record.key == current.key || family == users.VMess && strings.EqualFold(record.user.Email, email) {
			return nil, users.ErrConflict
		}
	}
	return current, nil
}
