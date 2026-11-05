package users_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/papilio-platform/Node/internal/users"
)

func TestShadowsocksAccountInputs(t *testing.T) {
	for _, method := range []users.ShadowsocksMethod{users.MethodAES128GCM, users.MethodAES256GCM, users.MethodChaCha20Poly1305, users.MethodXChaCha20Poly1305} {
		t.Run(string(method), func(t *testing.T) {
			c, _, native, target, account := commands(t)
			account.Protocol, account.ID, account.Password, account.Method = users.Shadowsocks, [16]byte{}, "isolated-password", method
			if result, err := c.Create(t.Context(), target, account); err != nil || result.Outcome != users.OutcomeApplied {
				t.Fatal("valid legacy account rejected", result, err)
			}
			for _, change := range []func(*users.Account){
				func(a *users.Account) { a.Password = "" },
				func(a *users.Account) { a.Password = strings.Repeat("x", 4097) },
				func(a *users.Account) { a.Password = string([]byte{0xff}) },
				func(a *users.Account) { a.ID[0] = 1 },
				func(a *users.Account) { a.Flow = users.FlowVision },
				func(a *users.Account) { a.Method = "" },
				func(a *users.Account) { a.Method = "none" },
				func(a *users.Account) { a.Method = "AES-128-GCM" },
				func(a *users.Account) { a.Method = "2022-blake3-aes-128-gcm" },
			} {
				bad := account
				change(&bad)
				native.steps = nil
				if _, err := c.Create(t.Context(), target, bad); !errors.Is(err, users.ErrInvalid) || len(native.steps) != 0 {
					t.Fatal("invalid legacy credential reached native access", err, native.steps)
				}
			}
		})
	}
	for _, family := range []users.Protocol{users.VMess, users.VLESS, users.Trojan} {
		c, _, native, target, account := commands(t)
		account.Protocol, account.Method = family, users.MethodAES128GCM
		if family == users.Trojan {
			account.ID, account.Password = [16]byte{}, "isolated-password"
		}
		if _, err := c.Create(t.Context(), target, account); !errors.Is(err, users.ErrInvalid) || len(native.steps) != 0 {
			t.Fatal("foreign method was ignored", err, native.steps)
		}
	}
}
