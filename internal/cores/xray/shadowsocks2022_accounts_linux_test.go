package xray

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/papilio-platform/Node/internal/users"
	"github.com/xtls/xray-core/app/proxyman/command"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	ss2022 "github.com/xtls/xray-core/proxy/shadowsocks_2022"
	"google.golang.org/protobuf/proto"
)

func TestSS2022NativePreparationAndAuthenticationIndex(t *testing.T) {
	a, session, native, account := accountFixture(t, users.Shadowsocks2022)
	account.Key, account.Password = "", "isolated-conversion-password"
	check, err := a.Check(t.Context(), session, "owned", account)
	if err != nil || check.Exists || check.Prepared.Key == "" || check.Prepared.Password != "" {
		t.Fatal("native binding did not prepare desired PSK", err)
	}
	if err := a.Add(t.Context(), session, "owned", check.Prepared); err != nil {
		t.Fatal(err)
	}
	var op command.AddUserOperation
	if err := proto.Unmarshal(native.operation.Value, &op); err != nil {
		t.Fatal(err)
	}
	var credential ss2022.Account
	if err := proto.Unmarshal(op.User.Account.Value, &credential); err != nil || credential.Key != check.Prepared.Key {
		t.Fatal("prepared PSK was not written", err)
	}
	native.count, native.response = 1, []*protocol.User{op.User}
	replay, err := a.Check(t.Context(), session, "owned", account)
	if err != nil || !replay.Equal || replay.Revision != check.DesiredRevision || replay.Prepared.Key != check.Prepared.Key {
		t.Fatal("password/PSK mapping or revision replay changed", err)
	}
	account.Password, account.Key, account.Email = "", credential.Key, "sibling"
	if _, err := a.Check(t.Context(), session, "owned", account); !errors.Is(err, users.ErrConflict) {
		t.Fatal("native user-key hash alias accepted", err)
	}
	if native.discovers != 3 || native.counts != 3 || native.reads != 3 || native.alters != 1 {
		t.Fatal("SS2022 preparation added hidden native I/O")
	}
}

func TestSS2022RejectsWrongContextBeforeFetchingUsers(t *testing.T) {
	for _, kind := range []string{"single", "relay", "chacha", "wrong-length"} {
		t.Run(kind, func(t *testing.T) {
			a, session, native, account := accountFixture(t, users.Shadowsocks2022)
			want := users.ErrUnsupported
			switch kind {
			case "single":
				native.inbounds[0].ProxySettings = serial.ToTypedMessage(&ss2022.ServerConfig{Method: ss2022.MethodAES128GCM})
			case "relay":
				native.inbounds[0].ProxySettings = serial.ToTypedMessage(&ss2022.RelayServerConfig{Method: ss2022.MethodAES128GCM})
			case "chacha":
				native.inbounds[0].ProxySettings = serial.ToTypedMessage(&ss2022.MultiUserServerConfig{Method: ss2022.MethodChaCha20Poly1305})
			case "wrong-length":
				account.Key = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
				want = users.ErrInvalid
			}
			if _, err := a.Check(t.Context(), session, "owned", account); !errors.Is(err, want) || native.discovers != 1 || native.counts != 0 || native.reads != 0 || native.alters != 0 {
				t.Fatal("invalid binding caused collection I/O or mutation", err)
			}
		})
	}
}

func TestSS2022UnindexedMembershipCannotBecomeSuccessfulAbsence(t *testing.T) {
	a, session, native, account := accountFixture(t, users.Shadowsocks2022)
	native.count = 1 // Native AddUser permits unnamed users absent from its email map.
	if _, err := a.Check(t.Context(), session, "owned", account); !errors.Is(err, users.ErrConflict) || native.alters != 0 {
		t.Fatal("unaccounted native authentication membership was treated as absent", err)
	}
}

func TestSS2022InspectionDoesNotReadPerUser(t *testing.T) {
	for _, size := range []int{0, 1, 16, 100} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			a, session, native, account := accountFixture(t, users.Shadowsocks2022)
			a.options.MaxUsers = 101
			native.count = int64(size)
			for i := range size {
				key := [16]byte{15: byte(i + 1)}
				native.response = append(native.response, &protocol.User{Email: fmt.Sprint(i), Account: serial.ToTypedMessage(&ss2022.Account{Key: base64.StdEncoding.EncodeToString(key[:])})})
			}
			if _, err := a.Check(t.Context(), session, "owned", account); err != nil || native.discovers != 1 || native.counts != 1 || native.reads != 1 || native.alters != 0 {
				t.Fatal("SS2022 prepared one request per user", err)
			}
		})
	}
}

func TestSS2022MalformedOrUnknownNativeCredentialsRemainProtected(t *testing.T) {
	for _, change := range []func(*ss2022.Account){
		func(a *ss2022.Account) { a.Key = "bad-short-key" },
		func(a *ss2022.Account) { a.Key = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))) },
		func(a *ss2022.Account) { a.ProtoReflect().SetUnknown([]byte{0x10, 1}) },
	} {
		a, session, native, account := accountFixture(t, users.Shadowsocks2022)
		credential := &ss2022.Account{Key: account.Key}
		change(credential)
		native.count, native.response = 1, []*protocol.User{{Email: account.Email, Account: serial.ToTypedMessage(credential)}}
		if _, err := a.Check(t.Context(), session, "owned", account); !errors.Is(err, users.ErrUnsupported) || native.alters != 0 {
			t.Fatal("unsupported native key state was overwritten", err)
		}
	}
}
