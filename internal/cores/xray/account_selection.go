package xray

import (
	"context"
	"strings"

	"github.com/papilio-platform/Node/internal/users"
	"github.com/xtls/xray-core/common/protocol"
)

type accountCollection struct {
	users []*protocol.User
	count int64
}

type selectedAccount struct {
	user        *protocol.User
	replaceable bool
}

func selectAccount(ctx context.Context, collection accountCollection, email, desiredKey string, binding accountBinding) (selectedAccount, error) {
	identity := strings.ToLower(email)
	current := -1
	conflict := false
	for i, user := range collection.users {
		if user == nil {
			continue // The credential pass rejects every invalid native record.
		}
		if strings.ToLower(user.Email) == identity || binding.family == users.Shadowsocks && strings.EqualFold(user.Email, email) {
			if current >= 0 {
				conflict = true
			} else {
				current = i
			}
		}
	}
	var selected selectedAccount
	var currentKey string
	if current >= 0 {
		key, replaceable, err := accountKey(collection.users[current], binding)
		if err != nil {
			return selectedAccount{}, err
		}
		currentKey = key
		selected = selectedAccount{user: collection.users[current], replaceable: replaceable}
	}
	for i, user := range collection.users {
		if err := ctx.Err(); err != nil {
			return selectedAccount{}, err
		}
		if i == current {
			continue
		}
		key, _, err := accountKey(user, binding)
		if err != nil {
			return selectedAccount{}, err
		}
		if desiredKey != "" && key == desiredKey || current >= 0 && key == currentKey || binding.family == users.VMess && strings.EqualFold(user.Email, email) {
			conflict = true
		}
	}
	// A conflict must not short-circuit validation of the remaining records.
	if int64(len(collection.users)) != collection.count || conflict {
		return selectedAccount{}, users.ErrConflict
	}
	return selected, nil
}
