package xray

import (
	"errors"
	"testing"

	"github.com/papilio-platform/Node/internal/users"
	"github.com/xtls/xray-core/app/proxyman/command"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/proxy/vless"
	"google.golang.org/protobuf/proto"
)

func TestVLESSFlowPayloadEqualityRevisionAndAliases(t *testing.T) {
	a, session, native, account := accountFixture(t, users.VLESS)
	account.Flow = users.FlowVision
	if err := a.Add(t.Context(), session, "owned", account); err != nil {
		t.Fatal(err)
	}
	var operation command.AddUserOperation
	if err := proto.Unmarshal(native.operation.Value, &operation); err != nil {
		t.Fatal(err)
	}
	var credential vless.Account
	if err := proto.Unmarshal(operation.User.Account.Value, &credential); err != nil || credential.Flow != vless.XRV {
		t.Fatal("Vision was lost from actual native mutation", err)
	}
	native.count, native.response = 1, []*protocol.User{operation.User}
	check, err := a.Check(t.Context(), session, "owned", account)
	if err != nil || !check.Equal || check.Revision != check.DesiredRevision {
		t.Fatal("Vision replay changed account", check, err)
	}
	account.Flow = users.FlowNone
	changed, err := a.Check(t.Context(), session, "owned", account)
	if err != nil || changed.Equal || changed.Revision != check.Revision || changed.DesiredRevision == check.Revision {
		t.Fatal("flow-only replacement lost its revision", changed, err)
	}
	account.Email = "alias"
	account.ID[6], account.ID[7] = 0xff, 0xff
	if _, err := a.Check(t.Context(), session, "owned", account); !errors.Is(err, users.ErrConflict) {
		t.Fatal("flow created another UUID authentication identity", err)
	}
	if native.discovers != 3 || native.counts != 3 || native.reads != 3 || native.alters != 1 {
		t.Fatal("flow added hidden native work", native.discovers, native.counts, native.reads, native.alters)
	}
}

func TestVLESSFlowDoesNotEraseOtherAccountSettings(t *testing.T) {
	for _, change := range []func(*vless.Account){
		func(a *vless.Account) { a.Flow = "unknown" },
		func(a *vless.Account) { a.Encryption = "other" },
		func(a *vless.Account) { a.Reverse = &vless.Reverse{Tag: "owned-reverse"} },
		func(a *vless.Account) { a.Testseed = []uint32{1, 2, 3, 4} },
	} {
		a, session, native, account := accountFixture(t, users.VLESS)
		credential := &vless.Account{Id: "00000000-0000-0000-0000-000000000001", Flow: vless.XRV}
		change(credential)
		native.count, native.response = 1, []*protocol.User{{Email: account.Email, Account: serial.ToTypedMessage(credential)}}
		if _, err := a.Check(t.Context(), session, "owned", account); !errors.Is(err, users.ErrUnsupported) || native.alters != 0 {
			t.Fatal("replacement would erase unsupported account behavior", err)
		}
	}
}
