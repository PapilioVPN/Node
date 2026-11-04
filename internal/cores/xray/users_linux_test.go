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
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type userClient struct {
	command.HandlerServiceClient
	reads, counts int
	selector      string
	response      []*protocol.User
	count         int64
	err           error
	block         bool
}

func (c *userClient) GetInboundUsers(ctx context.Context, req *command.GetInboundUserRequest, opts ...grpc.CallOption) (*command.GetInboundUserResponse, error) {
	c.reads++
	c.selector = req.Email
	if c.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &command.GetInboundUserResponse{Users: c.response}, c.err
}
func (c *userClient) GetInboundUsersCount(context.Context, *command.GetInboundUserRequest, ...grpc.CallOption) (*command.GetInboundUsersCountResponse, error) {
	c.counts++
	return &command.GetInboundUsersCountResponse{Count: c.count}, c.err
}
func userFixture(t *testing.T) (*UserReader, *Session, *userClient) {
	t.Helper()
	r, err := NewUserReader(1024, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c := &userClient{count: 2, response: []*protocol.User{{Email: "b", Level: 2, Account: &serial.TypedMessage{Type: "sensitive", Value: []byte("private-password")}}, {Email: "a", Level: 1}}}
	return r, &Session{api: &nativeAPI{handlers: c}}, c
}

func TestNativeUserQueriesUseFocusedSelectorsAndCounts(t *testing.T) {
	r, s, c := userFixture(t)
	c.response = c.response[:1]
	u, err := r.Get(t.Context(), s, "in", "b")
	if err != nil || u.Email != "b" || u.Level != 2 || c.reads != 1 || c.counts != 0 || c.selector != "b" {
		t.Fatalf("single query: %+v %v", u, err)
	}
	n, err := r.Count(t.Context(), s, "in")
	if err != nil || n != 2 || c.reads != 1 || c.counts != 1 {
		t.Fatal("count expanded users", err)
	}
	c.response = []*protocol.User{{Email: "b", Level: 2}, {Email: "a", Level: 1}}
	list, err := r.List(t.Context(), s, "in", 2)
	if err != nil || len(list) != 2 || list[0].Email != "a" || c.reads != 2 || c.counts != 2 || c.selector != "" {
		t.Fatalf("bounded collection: %+v %v", list, err)
	}
}

func TestNativeUserBoundsMissingPrivacyAndCancellation(t *testing.T) {
	r, s, c := userFixture(t)
	if _, err := NewUserReader(0, time.Second); err == nil {
		t.Fatal("unbounded response accepted")
	}
	if _, err := NewUserReader(1, -1); err == nil {
		t.Fatal("unbounded timeout accepted")
	}
	c.response = []*protocol.User{{}}
	if _, err := r.Get(t.Context(), s, "in", "missing"); !errors.Is(err, users.ErrNotFound) {
		t.Fatal("missing user fabricated", err)
	}
	c.count = 3
	if _, err := r.List(t.Context(), s, "in", 2); !errors.Is(err, users.ErrLimit) || c.reads != 1 {
		t.Fatal("oversized count fetched all", err)
	}
	c.count = 1
	c.response = []*protocol.User{{Email: "a"}, {Email: "b"}}
	if _, err := r.List(t.Context(), s, "in", 1); !errors.Is(err, users.ErrLimit) {
		t.Fatal("post-count growth silently truncated", err)
	}
	c.err = status.Error(codes.Unknown, "private-password")
	_, err := r.Count(t.Context(), s, "in")
	if err == nil || strings.Contains(err.Error(), "private-password") || status.Code(err) != codes.Unknown {
		t.Fatal("native error was leaked or lost", err)
	}
	c.err = status.Error(codes.ResourceExhausted, "private-password")
	if _, err := r.Get(t.Context(), s, "in", "b"); !errors.Is(err, users.ErrLimit) {
		t.Fatal("native byte cap hidden", err)
	}
	if _, err := r.Count(t.Context(), nil, "in"); !errors.Is(err, users.ErrUnsupported) {
		t.Fatal("unsupported session", err)
	}
	c.err = nil
	c.block = true
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.Get(ctx, s, "in", "b"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}

func TestNativeUserCollectionCallCountDoesNotGrowPerUser(t *testing.T) {
	for _, size := range []int{1, 2, 16, 100} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			r, s, c := userFixture(t)
			c.count = int64(size)
			c.response = make([]*protocol.User, size)
			for i := range c.response {
				c.response[i] = &protocol.User{Email: fmt.Sprintf("user-%03d", size-i)}
			}
			result, err := r.List(t.Context(), s, "in", size)
			if err != nil || len(result) != size || c.counts != 1 || c.reads != 1 {
				t.Fatalf("per-user RPC growth: size=%d reads=%d counts=%d %v", size, c.reads, c.counts, err)
			}
		})
	}
}

func TestNativeUserDeadlineAndUnknownCount(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, s, c := userFixture(t)
		c.block = true
		if _, err := r.Get(t.Context(), s, "in", "account"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("native query deadline lost", err)
		}
		c.block = false
		c.count = -1
		if _, err := r.Count(t.Context(), s, "in"); err == nil {
			t.Fatal("negative count became a successful zero")
		}
	})
}
