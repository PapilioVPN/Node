package xray

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/papilio-platform/Node/internal/users"
	"github.com/xtls/xray-core/app/proxyman/command"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/proxy/trojan"
	"github.com/xtls/xray-core/proxy/vless"
	"github.com/xtls/xray-core/proxy/vmess"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type accountClient struct {
	userClient
	inbounds          []*core.InboundHandlerConfig
	discovers, alters int
	operation         *serial.TypedMessage
	blockList         bool
	nilList, nilAlter bool
}

func (c *accountClient) ListInbounds(ctx context.Context, _ *command.ListInboundsRequest, _ ...grpc.CallOption) (*command.ListInboundsResponse, error) {
	c.discovers++
	if c.blockList {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if c.nilList {
		return nil, nil
	}
	return &command.ListInboundsResponse{Inbounds: c.inbounds}, c.err
}
func (c *accountClient) AlterInbound(_ context.Context, req *command.AlterInboundRequest, _ ...grpc.CallOption) (*command.AlterInboundResponse, error) {
	c.alters++
	c.operation = req.Operation
	if c.nilAlter {
		return nil, nil
	}
	return &command.AlterInboundResponse{}, c.err
}

func TestAccountInspectionBoundsAndInvalidNativeRecords(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*UserAccounts, *accountClient)
		want    error
	}{
		{"missing-inbound", func(_ *UserAccounts, c *accountClient) { c.inbounds = nil }, users.ErrNotFound},
		{"inbound-limit", func(a *UserAccounts, c *accountClient) {
			a.options.MaxInbounds = 1
			c.inbounds = append(c.inbounds, c.inbounds[0])
		}, users.ErrLimit},
		{"growth-after-count", func(_ *UserAccounts, c *accountClient) { c.response = make([]*protocol.User, 101) }, users.ErrLimit},
		{"nil-entry", func(_ *UserAccounts, c *accountClient) { c.response = []*protocol.User{nil} }, users.ErrUnsupported},
		{"wrong-account-type", func(_ *UserAccounts, c *accountClient) {
			c.response = []*protocol.User{{Email: "other", Account: serial.ToTypedMessage(&trojan.Account{Password: "synthetic"})}}
		}, users.ErrUnsupported},
		{"invalid-native-id", func(_ *UserAccounts, c *accountClient) {
			c.response = []*protocol.User{{Email: "other", Account: serial.ToTypedMessage(&vless.Account{Id: ""})}}
		}, users.ErrUnsupported},
		{"duplicate-identity", func(_ *UserAccounts, c *accountClient) {
			c.response = []*protocol.User{{Email: "Alice", Account: serial.ToTypedMessage(&vless.Account{Id: "one"})}, {Email: "ALICE", Account: serial.ToTypedMessage(&vless.Account{Id: "two"})}}
		}, users.ErrConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, s, c, account := accountFixture(t, users.VLESS)
			test.prepare(a, c)
			if _, err := a.Check(t.Context(), s, "owned", account); !errors.Is(err, test.want) || c.alters != 0 {
				t.Fatal("unsafe inspection admitted", err)
			}
		})
	}
}

func TestAccountCapacityCancellationAndMissingReplies(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, s, c, account := accountFixture(t, users.Trojan)
		a.options.MaxUsers = 1
		c.count = 1
		c.response = []*protocol.User{{Email: "other", Account: serial.ToTypedMessage(&trojan.Account{Password: "other"})}}
		if check, err := a.Check(t.Context(), s, "owned", account); err != nil || check.Exists || !check.AtCapacity {
			t.Fatal("native capacity facts lost", check, err)
		}
		if exists, err := a.CheckRemoval(t.Context(), s, "owned", "other"); err != nil || !exists.Exists {
			t.Fatal("cannot remove at capacity", err)
		}
		c.nilList = true
		if _, err := a.Check(t.Context(), s, "owned", account); err == nil {
			t.Fatal("missing reply fabricated absence")
		}
		c.nilList = false
		c.blockList = true
		if _, err := a.Check(t.Context(), s, "owned", account); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("unbounded inspection", err)
		}
		c.blockList = false
		c.nilAlter = true
		if err := a.Add(t.Context(), s, "owned", account); err == nil {
			t.Fatal("missing acknowledgement fabricated success")
		}
		before := c.alters
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := a.Remove(ctx, s, "owned", "Alice"); !errors.Is(err, context.Canceled) || c.alters != before {
			t.Fatal("pre-cancelled write sent", err)
		}
	})
}
func accountFixture(t *testing.T, family users.Protocol) (*UserAccounts, *Session, *accountClient, users.Account) {
	t.Helper()
	a, err := NewUserAccounts(UserAccountOptions{ReplyBytes: 1 << 20, MaxUsers: 100, MaxInbounds: 10, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	kind := map[users.Protocol]string{users.VMess: "vmess", users.VLESS: "vless", users.Trojan: "trojan"}[family]
	c := &accountClient{inbounds: []*core.InboundHandlerConfig{{Tag: "owned", ProxySettings: &serial.TypedMessage{Type: "xray.proxy." + kind + ".inbound.Config"}}}}
	if family == users.Trojan {
		c.inbounds[0].ProxySettings.Type = "xray.proxy.trojan.ServerConfig"
	}
	account := users.Account{Email: "Alice", Level: 2, Protocol: family, ID: [16]byte{15: 1}}
	if family == users.Trojan {
		account.ID = [16]byte{}
		account.Password = "isolated-test-password"
	}
	return a, &Session{api: &nativeAPI{handlers: c}}, c, account
}

func TestAccountNativePayloadAndCompleteEquality(t *testing.T) {
	for _, family := range []users.Protocol{users.VMess, users.VLESS, users.Trojan} {
		t.Run(fmt.Sprint(family), func(t *testing.T) {
			a, s, c, account := accountFixture(t, family)
			if err := a.Add(t.Context(), s, "owned", account); err != nil || c.alters != 1 || c.operation == nil {
				t.Fatal("missing native add", err)
			}
			var op command.AddUserOperation
			if err := proto.Unmarshal(c.operation.Value, &op); err != nil {
				t.Fatal(err)
			}
			if op.User.Email != "Alice" || op.User.Level != 2 || op.User.Account == nil {
				t.Fatal("account metadata lost")
			}
			memory, err := op.User.ToMemoryUser()
			if err != nil {
				t.Fatal(err)
			}
			c.response = []*protocol.User{protocol.ToProtoUser(memory)}
			c.count = 1
			check, err := a.Check(t.Context(), s, "owned", account)
			if err != nil || !check.Exists || !check.Equal {
				t.Fatal("native canonical no-op", check, err)
			}
			account.Email = "aLiCe"
			check, err = a.Check(t.Context(), s, "owned", account)
			if err != nil || !check.Equal {
				t.Fatal("native identity case changed", check, err)
			}
			account.Level++
			check, err = a.Check(t.Context(), s, "owned", account)
			if err != nil || !check.Exists || check.Equal {
				t.Fatal("level update lost", check, err)
			}
			if exists, err := a.CheckRemoval(t.Context(), s, "owned", "alice"); err != nil || !exists.Exists {
				t.Fatal("removal check", exists, err)
			}
			if err := a.Remove(t.Context(), s, "owned", "Alice"); err != nil || c.alters != 2 {
				t.Fatal("native remove", err)
			}
			var remove command.RemoveUserOperation
			if err := proto.Unmarshal(c.operation.Value, &remove); err != nil || remove.Email != "Alice" {
				t.Fatal("wrong removal identity", err)
			}
		})
	}
}

func TestAccountNativeCredentialCollisions(t *testing.T) {
	for _, family := range []users.Protocol{users.VMess, users.VLESS, users.Trojan} {
		t.Run(fmt.Sprint(family), func(t *testing.T) {
			a, s, c, account := accountFixture(t, family)
			var native *serial.TypedMessage
			switch family {
			case users.VMess:
				native = serial.ToTypedMessage(&vmess.Account{Id: "00000000-0000-0000-0000-000000000001"})
			case users.VLESS:
				native = serial.ToTypedMessage(&vless.Account{Id: "00000000-0000-ffff-0000-000000000001"})
			case users.Trojan:
				native = serial.ToTypedMessage(&trojan.Account{Password: account.Password})
			}
			c.count = 1
			c.response = []*protocol.User{{Email: "sibling", Account: native}}
			if _, err := a.Check(t.Context(), s, "owned", account); !errors.Is(err, users.ErrConflict) {
				t.Fatal("credential alias accepted", err)
			}
			c.response = append(c.response, &protocol.User{Email: "Alice", Account: native})
			c.count = 2
			if _, err := a.CheckRemoval(t.Context(), s, "owned", "Alice"); !errors.Is(err, users.ErrConflict) {
				t.Fatal("aliased removal could revoke sibling", err)
			}
			if c.alters != 0 {
				t.Fatal("inspection mutated engine")
			}
		})
	}
}

func TestAccountIdentityFoldingMatchesNativeRemoval(t *testing.T) {
	for _, family := range []users.Protocol{users.VMess, users.VLESS, users.Trojan} {
		t.Run(fmt.Sprint(family), func(t *testing.T) {
			a, s, c, account := accountFixture(t, family)
			first, err := encodeAccount(account)
			if err != nil {
				t.Fatal(err)
			}
			first.Email = "s"
			account.ID[15] = 2
			if family == users.Trojan {
				account.ID = [16]byte{}
				account.Password = "other-password"
			}
			second, err := encodeAccount(account)
			if err != nil {
				t.Fatal(err)
			}
			second.Email = "ſ"
			c.response = []*protocol.User{first, second}
			c.count = 2
			check, err := a.CheckRemoval(t.Context(), s, "owned", "ſ")
			if family == users.VMess {
				if !errors.Is(err, users.ErrConflict) {
					t.Fatal("ambiguous VMess native removal allowed", err)
				}
			} else if err != nil || !check.Exists {
				t.Fatal("distinct native lowercased identities were conflated", err)
			}
		})
	}
}

func TestAccountNativeBoundsTypesAndPrivacy(t *testing.T) {
	a, s, c, account := accountFixture(t, users.VLESS)
	c.count = 101
	if _, err := a.Check(t.Context(), s, "owned", account); !errors.Is(err, users.ErrLimit) || c.reads != 0 {
		t.Fatal("unbounded account observation", err)
	}
	c.count = 0
	c.inbounds[0].ProxySettings.Type = "xray.proxy.http.ServerConfig"
	if _, err := a.Check(t.Context(), s, "owned", account); !errors.Is(err, users.ErrUnsupported) {
		t.Fatal("unsafe account type allowed", err)
	}
	c.inbounds[0].ProxySettings.Type = "xray.proxy.vless.inbound.Config"
	c.response = []*protocol.User{{Email: "Alice", Account: serial.ToTypedMessage(&vless.Account{Id: "00000000-0000-0000-0000-000000000001", Flow: "xtls-rprx-vision"})}}
	c.count = 1
	if _, err := a.Check(t.Context(), s, "owned", account); !errors.Is(err, users.ErrUnsupported) {
		t.Fatal("basic replacement could erase advanced account", err)
	}
	c.err = status.Error(codes.Unknown, "private-password")
	err := a.Add(t.Context(), s, "owned", account)
	if err == nil || strings.Contains(err.Error(), "private-password") || status.Code(err) != codes.Unknown {
		t.Fatal("native mutation error leaked/lost", err)
	}
	if _, err := a.CheckRemoval(t.Context(), nil, "owned", "Alice"); !errors.Is(err, users.ErrUnsupported) {
		t.Fatal("unsupported session", err)
	}
}

func TestAccountInspectionDoesNotReadPerUser(t *testing.T) {
	for _, size := range []int{0, 1, 16, 100} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			a, s, c, account := accountFixture(t, users.Trojan)
			a.options.MaxUsers = 101
			c.count = int64(size)
			for i := range size {
				c.response = append(c.response, &protocol.User{Email: fmt.Sprint(i), Account: serial.ToTypedMessage(&trojan.Account{Password: fmt.Sprint(i)})})
			}
			if _, err := a.Check(t.Context(), s, "owned", account); err != nil || c.discovers != 1 || c.counts != 1 || c.reads != 1 {
				t.Fatal("per-user native work", err, c.discovers, c.counts, c.reads)
			}
		})
	}
}

func FuzzAccountCredentialKey(f *testing.F) {
	f.Add(byte(0), serial.ToTypedMessage(&vmess.Account{Id: "00000000-0000-4000-8000-000000000001"}).Value)
	f.Add(byte(1), serial.ToTypedMessage(&vless.Account{Id: "00000000-0000-4000-8000-000000000001"}).Value)
	f.Add(byte(2), serial.ToTypedMessage(&trojan.Account{Password: "isolated-test-password"}).Value)
	f.Add(byte(1), []byte{0xff})
	f.Fuzz(func(t *testing.T, selector byte, data []byte) {
		if len(data) > 65536 {
			return
		}
		family := users.Protocol(selector%3 + 1)
		var message proto.Message
		switch family {
		case users.VMess:
			message = &vmess.Account{}
		case users.VLESS:
			message = &vless.Account{}
		case users.Trojan:
			message = &trojan.Account{}
		}
		key, _, err := accountKey(&protocol.User{Account: &serial.TypedMessage{Type: serial.GetMessageType(message), Value: data}}, family)
		if err == nil {
			want := 16
			if family == users.Trojan {
				want = 28
			}
			if len(key) != want {
				t.Fatalf("invalid native authentication key size: %d", len(key))
			}
		}
	})
}
