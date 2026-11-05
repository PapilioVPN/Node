package xray

import (
	"errors"
	"fmt"
	"testing"

	"github.com/papilio-platform/Node/internal/users"
	"github.com/xtls/xray-core/app/proxyman/command"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/proxy/shadowsocks"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestShadowsocksNativePayloadEqualityAndCredentialScope(t *testing.T) {
	for method, cipher := range map[users.ShadowsocksMethod]shadowsocks.CipherType{
		users.MethodAES128GCM:         shadowsocks.CipherType_AES_128_GCM,
		users.MethodAES256GCM:         shadowsocks.CipherType_AES_256_GCM,
		users.MethodChaCha20Poly1305:  shadowsocks.CipherType_CHACHA20_POLY1305,
		users.MethodXChaCha20Poly1305: shadowsocks.CipherType_XCHACHA20_POLY1305,
	} {
		t.Run(string(method), func(t *testing.T) {
			a, session, native, account := accountFixture(t, users.Shadowsocks)
			account.Method = method
			if err := a.Add(t.Context(), session, "owned", account); err != nil {
				t.Fatal(err)
			}
			var op command.AddUserOperation
			if err := proto.Unmarshal(native.operation.Value, &op); err != nil {
				t.Fatal(err)
			}
			var credential shadowsocks.Account
			if err := proto.Unmarshal(op.User.Account.Value, &credential); err != nil || credential.CipherType != cipher || credential.Password != account.Password {
				t.Fatal("legacy native method/password lost", err)
			}
			native.count, native.response = 1, []*protocol.User{op.User}
			check, err := a.Check(t.Context(), session, "owned", account)
			if err != nil || !check.Equal || check.Revision != check.DesiredRevision {
				t.Fatal("native legacy replay", check, err)
			}
			account.Password += "-replacement"
			changed, err := a.Check(t.Context(), session, "owned", account)
			if err != nil || changed.Equal || check.Revision != changed.Revision || changed.DesiredRevision == check.Revision {
				t.Fatal("legacy password fence lost", changed, err)
			}
			account.Password, account.Email = credential.Password, "sibling"
			if _, err := a.Check(t.Context(), session, "owned", account); !errors.Is(err, users.ErrConflict) {
				t.Fatal("shared native authentication key accepted", err)
			}
			if account.Method == users.MethodAES128GCM {
				account.Method = users.MethodAES256GCM
			} else {
				account.Method = users.MethodAES128GCM
			}
			if _, err := a.Check(t.Context(), session, "owned", account); err != nil {
				t.Fatal("different cipher conflated with authentication alias", err)
			}
			if native.discovers != 4 || native.counts != 4 || native.reads != 4 || native.alters != 1 {
				t.Fatal("legacy account added hidden native work")
			}
		})
	}
}

func TestShadowsocksIdentityMatchesNativeCaseFolding(t *testing.T) {
	a, session, native, account := accountFixture(t, users.Shadowsocks)
	account.Email = "s"
	first := &protocol.User{Email: "s", Level: account.Level, Account: serial.ToTypedMessage(&shadowsocks.Account{CipherType: shadowsocks.CipherType_AES_128_GCM, Password: account.Password})}
	native.count, native.response = 1, []*protocol.User{first}
	account.Email = "ſ"
	check, err := a.Check(t.Context(), session, "owned", account)
	if err != nil || !check.Exists || !check.Equal {
		t.Fatal("native EqualFold identity was treated as absence", check, err)
	}
	removal, err := a.CheckRemoval(t.Context(), session, "owned", account.Email)
	if err != nil || !removal.Exists || removal.Revision != check.Revision {
		t.Fatal("native equivalent removal was treated as absence", removal, err)
	}
	native.count = 2
	native.response = append(native.response, &protocol.User{Email: "ſ", Account: serial.ToTypedMessage(&shadowsocks.Account{CipherType: shadowsocks.CipherType_AES_128_GCM, Password: "other-password"})})
	if _, err := a.CheckRemoval(t.Context(), session, "owned", account.Email); !errors.Is(err, users.ErrConflict) {
		t.Fatal("ambiguous native removal admitted", err)
	}
}

func TestShadowsocksInspectionHasNoPerUserRPC(t *testing.T) {
	for _, size := range []int{0, 1, 16, 100} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			a, session, native, account := accountFixture(t, users.Shadowsocks)
			a.options.MaxUsers = 101
			native.count = int64(size)
			for i := range size {
				native.response = append(native.response, &protocol.User{Email: fmt.Sprint(i), Account: serial.ToTypedMessage(&shadowsocks.Account{CipherType: shadowsocks.CipherType_AES_128_GCM, Password: fmt.Sprint(i)})})
			}
			if _, err := a.Check(t.Context(), session, "owned", account); err != nil || native.discovers != 1 || native.counts != 1 || native.reads != 1 || native.alters != 0 {
				t.Fatal("legacy inspection sent per-user I/O", err)
			}
		})
	}
}

func TestShadowsocksUnsupportedNativeAccountsRemainProtected(t *testing.T) {
	for _, change := range []func(*shadowsocks.Account){
		func(a *shadowsocks.Account) { a.CipherType = shadowsocks.CipherType(9) }, // Historical none value; absent from the pinned SDK.
		func(a *shadowsocks.Account) { a.CipherType = shadowsocks.CipherType_UNKNOWN },
		func(a *shadowsocks.Account) { a.IvCheck = true },
		func(a *shadowsocks.Account) {
			a.ProtoReflect().SetUnknown(protowire.AppendVarint(protowire.AppendTag(nil, 99, protowire.VarintType), 1))
		},
	} {
		a, session, native, account := accountFixture(t, users.Shadowsocks)
		credential := &shadowsocks.Account{CipherType: shadowsocks.CipherType_AES_128_GCM, Password: account.Password}
		change(credential)
		native.count, native.response = 1, []*protocol.User{{Email: account.Email, Account: serial.ToTypedMessage(credential)}}
		if _, err := a.Check(t.Context(), session, "owned", account); !errors.Is(err, users.ErrUnsupported) || native.alters != 0 {
			t.Fatal("unsupported native credential could be overwritten", err)
		}
	}
}
