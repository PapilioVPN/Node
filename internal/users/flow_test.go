package users_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/papilio-platform/Node/internal/users"
)

func TestAccountFlowValidationBeforeNativeAccess(t *testing.T) {
	for _, test := range []struct {
		protocol users.Protocol
		flow     users.VLESSFlow
	}{
		{users.VLESS, "unknown"},
		{users.VLESS, "xtls-rprx-vision-udp443"},
		{users.VLESS, "xtls-rprx-direct"},
		{users.VMess, users.FlowVision},
		{users.Trojan, users.FlowVision},
	} {
		t.Run(fmt.Sprintf("%d/%s", test.protocol, test.flow), func(t *testing.T) {
			commands, _, native, target, account := commands(t)
			account.Protocol, account.Flow = test.protocol, test.flow
			if account.Protocol == users.Trojan {
				account.ID, account.Password = [16]byte{}, "isolated-password"
			}
			if _, err := commands.Create(t.Context(), target, account); !errors.Is(err, users.ErrInvalid) || len(native.steps) != 0 {
				t.Fatal("invalid flow reached native access", err, native.steps)
			}
		})
	}
}
