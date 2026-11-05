package xray

import (
	"context"
	"errors"

	"github.com/papilio-platform/Node/internal/instance"
	"github.com/papilio-platform/Node/internal/users"
	"github.com/xtls/xray-core/app/proxyman/command"
	"github.com/xtls/xray-core/common/serial"
	"google.golang.org/grpc"
)

// Add performs one AddUserOperation after a successful Check under the same
// execution access. It does not repeat the check, retry or replace an account.
func (a *UserAccounts) Add(ctx context.Context, session instance.ISession, inbound string, account users.Account) error {
	user, err := encodeAccount(account)
	if err != nil {
		return err
	}
	return a.alterAccount(ctx, session, inbound, serial.ToTypedMessage(&command.AddUserOperation{User: user}))
}

// Remove performs one RemoveUserOperation after CheckRemoval/Check confirms an
// unaliased existing identity under the same execution access.
func (a *UserAccounts) Remove(ctx context.Context, session instance.ISession, inbound, email string) error {
	if email == "" {
		return users.ErrInvalid
	}
	return a.alterAccount(ctx, session, inbound, serial.ToTypedMessage(&command.RemoveUserOperation{Email: email}))
}

func (a *UserAccounts) alterAccount(ctx context.Context, session instance.ISession, inbound string, operation *serial.TypedMessage) error {
	api, err := userAPIFor(session)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, a.options.Timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	response, err := api.handlers.AlterInbound(ctx, &command.AlterInboundRequest{Tag: inbound, Operation: operation}, grpc.MaxCallRecvMsgSize(a.options.ReplyBytes))
	if err != nil {
		return errors.Join(ctx.Err(), &UserMutationError{cause: err})
	}
	if response == nil {
		return errors.New("missing Xray account mutation acknowledgement")
	}
	return nil
}

// UserMutationError excludes sensitive native text from default error formatting.
type UserMutationError struct{ cause error }

func (e *UserMutationError) Error() string { return "Xray user mutation failed" }

// Unwrap preserves the original status/cause for explicit inspection.
func (e *UserMutationError) Unwrap() error { return e.cause }
