package users_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/papilio-platform/Node/internal/users"
)

func TestShadowsocks2022InputAndPreparedWrite(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	for _, password := range []bool{false, true} {
		c, _, native, target, account := commands(t)
		account.Protocol, account.ID, account.Key = users.Shadowsocks2022, [16]byte{}, key
		if password {
			account.Key, account.Password = "", "isolated-password"
		}
		prepared := account
		prepared.Key, prepared.Password = key, ""
		native.prepared = &prepared
		result, err := c.Create(t.Context(), target, account)
		if err != nil || native.added != prepared || result.Key != key {
			t.Fatal("prepared PSK was not the actual write/result", result, err)
		}
		native.check.Exists, native.check.Equal = true, true
		result, err = c.Replace(t.Context(), target, "old", account)
		if err != nil || result.Outcome != users.OutcomeUnchanged || result.Key != key {
			t.Fatal("PSK replay lost prepared key", result, err)
		}
		native.check.Equal = false
		native.writeErr = errors.New("write acknowledgement lost")
		result, err = c.Replace(t.Context(), target, "old", account)
		if err == nil || result.Outcome != users.OutcomeUnknown || result.Key != "" {
			t.Fatal("unknown effect returned an applied key", result, err)
		}
	}
}

func TestShadowsocks2022RejectsInvalidInputsBeforeAccess(t *testing.T) {
	c, _, native, target, account := commands(t)
	account.Protocol, account.ID = users.Shadowsocks2022, [16]byte{}
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	for _, change := range []func(*users.Account){
		func(a *users.Account) {},
		func(a *users.Account) { a.Key = "raw-or-invalid-key" },
		func(a *users.Account) { a.Key = base64.StdEncoding.EncodeToString([]byte("short")) },
		func(a *users.Account) { a.Key = key + "\n" },
		func(a *users.Account) { a.Key, a.Password = key, "both" },
		func(a *users.Account) { a.Key, a.ID[0] = key, 1 },
		func(a *users.Account) { a.Key, a.Flow = key, users.FlowVision },
		func(a *users.Account) { a.Key, a.Method = key, users.MethodAES128GCM },
		func(a *users.Account) { a.Password = strings.Repeat("x", 4097) },
		func(a *users.Account) { a.Password = string([]byte{0xff}) },
	} {
		bad := account
		change(&bad)
		if _, err := c.Create(t.Context(), target, bad); !errors.Is(err, users.ErrInvalid) || len(native.steps) != 0 {
			t.Fatal("invalid SS2022 request reached native inspection", err)
		}
	}
	account.Protocol, account.Key = users.Trojan, key
	account.Password = "valid-trojan-password"
	if _, err := c.Create(t.Context(), target, account); !errors.Is(err, users.ErrInvalid) || len(native.steps) != 0 {
		t.Fatal("foreign PSK was ignored", err)
	}
}
