package xray

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/papilio-platform/Node/internal/instance"
	"github.com/papilio-platform/Node/internal/users"
	"github.com/xtls/xray-core/app/proxyman/command"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UserReader bounds native metadata replies and each complete query's lifetime.
type UserReader struct {
	replyBytes int
	timeout    time.Duration
}

// NewUserReader accepts 1 byte..64 MiB per response and a positive timeout.
func NewUserReader(replyBytes int, timeout time.Duration) (*UserReader, error) {
	if replyBytes < 1 || replyBytes > 64<<20 || timeout <= 0 {
		return nil, users.ErrInvalid
	}
	return &UserReader{replyBytes: replyBytes, timeout: timeout}, nil
}

// Get uses the native email selector; credentials are excluded from its result.
func (r *UserReader) Get(ctx context.Context, session instance.ISession, inbound, email string) (users.User, error) {
	if email == "" {
		return users.User{}, users.ErrInvalid
	}
	api, err := userAPIFor(session)
	if err != nil {
		return users.User{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	response, err := api.handlers.GetInboundUsers(ctx, &command.GetInboundUserRequest{Tag: inbound, Email: email}, grpc.MaxCallRecvMsgSize(r.replyBytes))
	if err != nil {
		return users.User{}, userQueryError(ctx, err)
	}
	if response == nil {
		return users.User{}, errors.New("missing Xray user response")
	}
	if len(response.Users) == 0 || len(response.Users) == 1 && (response.Users[0] == nil || response.Users[0].Email == "") {
		return users.User{}, users.ErrNotFound
	}
	if len(response.Users) != 1 {
		return users.User{}, errors.New("invalid Xray single-user response")
	}
	u := response.Users[0]
	revision, err := fingerprintAccount(api.revisionKey, u)
	if err != nil {
		return users.User{}, err
	}
	return users.User{Email: u.Email, Level: u.Level, Revision: revision}, nil
}

// Count uses native count rather than materializing account records.
func (r *UserReader) Count(ctx context.Context, session instance.ISession, inbound string) (int64, error) {
	api, err := userAPIFor(session)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return readUserCount(ctx, api.handlers, inbound, r.replyBytes)
}

func readUserCount(ctx context.Context, client command.HandlerServiceClient, inbound string, replyBytes int) (int64, error) {
	response, err := client.GetInboundUsersCount(ctx, &command.GetInboundUserRequest{Tag: inbound}, grpc.MaxCallRecvMsgSize(replyBytes))
	if err != nil {
		return 0, userQueryError(ctx, err)
	}
	if response == nil || response.Count < 0 {
		return 0, errors.New("invalid Xray user count response")
	}
	return response.Count, nil
}

// List count-preflights the native non-paginated response and rejects item/byte
// overflow. It sorts metadata by email/level and promises no atomic count+read.
func (r *UserReader) List(ctx context.Context, session instance.ISession, inbound string, limit int) ([]users.User, error) {
	if limit < 1 || limit > users.MaxListUsers {
		return nil, users.ErrInvalid
	}
	api, err := userAPIFor(session)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	count, err := readUserCount(ctx, api.handlers, inbound, r.replyBytes)
	if err != nil {
		return nil, err
	}
	if count > int64(limit) {
		return nil, users.ErrLimit
	}
	response, err := api.handlers.GetInboundUsers(ctx, &command.GetInboundUserRequest{Tag: inbound}, grpc.MaxCallRecvMsgSize(r.replyBytes))
	if err != nil {
		return nil, userQueryError(ctx, err)
	}
	if response == nil {
		return nil, errors.New("missing Xray user collection response")
	}
	if len(response.Users) > limit {
		return nil, users.ErrLimit
	}
	result := make([]users.User, 0, len(response.Users))
	for _, u := range response.Users {
		if u == nil {
			return nil, errors.New("invalid Xray user collection entry")
		}
		revision, err := fingerprintAccount(api.revisionKey, u)
		if err != nil {
			return nil, err
		}
		result = append(result, users.User{Email: u.Email, Level: u.Level, Revision: revision})
	}
	slices.SortFunc(result, func(a, b users.User) int {
		return cmp.Or(cmp.Compare(a.Email, b.Email), cmp.Compare(a.Level, b.Level))
	})
	return result, nil
}

func userAPIFor(session instance.ISession) (*nativeAPI, error) {
	native, ok := session.(*Session)
	if !ok || native == nil || native.api == nil || native.api.handlers == nil {
		return nil, users.ErrUnsupported
	}
	return native.api, nil
}

// UserQueryError keeps potentially sensitive native text out of normal errors.
// Unwrap permits explicit inspection of the original gRPC status and cause.
type UserQueryError struct{ cause error }

func (e *UserQueryError) Error() string { return "Xray user query failed" }

// Unwrap preserves the original native status and potentially sensitive cause.
func (e *UserQueryError) Unwrap() error { return e.cause }

func userQueryError(ctx context.Context, err error) error {
	cause := &UserQueryError{cause: err}
	if status.Code(err) == codes.ResourceExhausted {
		return errors.Join(users.ErrLimit, ctx.Err(), cause)
	}
	return errors.Join(ctx.Err(), cause)
}
