package users_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/papilio-platform/Node/internal/instance"
	"github.com/papilio-platform/Node/internal/users"
)

type accounts struct {
	check              users.AccountCheck
	checkErr, writeErr error
	removeErr          error
	steps              []string
	block              bool
	blockAdd           bool
	afterRemove        context.CancelFunc
}

func (a *accounts) Check(ctx context.Context, _ instance.ISession, _ string, _ users.Account) (users.AccountCheck, error) {
	a.steps = append(a.steps, "check")
	if a.block {
		<-ctx.Done()
		return users.AccountCheck{}, ctx.Err()
	}
	return a.check, a.checkErr
}
func (a *accounts) CheckRemoval(context.Context, instance.ISession, string, string) (users.RemovalCheck, error) {
	a.steps = append(a.steps, "check-removal")
	return users.RemovalCheck{Exists: a.check.Exists, Revision: a.check.Revision}, a.checkErr
}
func (a *accounts) Add(ctx context.Context, _ instance.ISession, _ string, _ users.Account) error {
	a.steps = append(a.steps, "add")
	if a.blockAdd {
		<-ctx.Done()
		return ctx.Err()
	}
	return a.writeErr
}

func (a *accounts) Remove(context.Context, instance.ISession, string, string) error {
	a.steps = append(a.steps, "remove")
	if a.afterRemove != nil {
		a.afterRemove()
	}
	return a.removeErr
}
func commands(t *testing.T) (*users.Commands, *instance.Service, *accounts, users.Target, users.Account) {
	t.Helper()
	_, life, _, target := fixture(t)
	a := &accounts{check: users.AccountCheck{Revision: "old", DesiredRevision: "new"}}
	c, err := users.NewCommands(life, a, a, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c, life, a, target, users.Account{Email: "account", Level: 1, Protocol: users.VMess, ID: [16]byte{15: 1}}
}

func TestAccountCommandsCreateReplaceAndReplay(t *testing.T) {
	c, _, a, target, account := commands(t)
	result, err := c.Create(t.Context(), target, account)
	if err != nil || result.Outcome != users.OutcomeApplied || !slices.Equal(a.steps, []string{"check", "add"}) {
		t.Fatalf("create: %+v %v %v", result, err, a.steps)
	}
	a.check = users.AccountCheck{Exists: true, Equal: true, Revision: "old", DesiredRevision: "new"}
	a.steps = nil
	for _, run := range []func(context.Context, users.Target, users.Account) (users.MutationResult, error){c.Create, func(ctx context.Context, target users.Target, a users.Account) (users.MutationResult, error) {
		return c.Replace(ctx, target, "old", a)
	}} {
		if r, err := run(t.Context(), target, account); err != nil || r.Outcome != users.OutcomeUnchanged || r.PreviousRemoved {
			t.Fatal("equal replay changed account", r, err)
		}
	}
	if !slices.Equal(a.steps, []string{"check", "check"}) {
		t.Fatal("replay sent native mutations", a.steps)
	}
	a.check.Equal = false
	if _, err := c.Create(t.Context(), target, account); !errors.Is(err, users.ErrConflict) {
		t.Fatal("create overwrote account", err)
	}
	a.steps = nil
	result, err = c.Replace(t.Context(), target, "old", account)
	if err != nil || result.Outcome != users.OutcomeApplied || !result.PreviousRemoved || !slices.Equal(a.steps, []string{"check", "remove", "add"}) {
		t.Fatal("replacement effects/order", result, err, a.steps)
	}
	a.check.Exists = false
	a.steps = nil
	if _, err := c.Replace(t.Context(), target, "old", account); !errors.Is(err, users.ErrNotFound) || len(a.steps) != 1 {
		t.Fatal("replace created missing identity", err)
	}
}

func TestAccountRemovalAndPartialOutcomes(t *testing.T) {
	c, _, a, target, account := commands(t)
	r, err := c.Remove(t.Context(), target, account.Email, "old")
	if err != nil || r.Outcome != users.OutcomeAbsent || r.PreviousRemoved || !slices.Equal(a.steps, []string{"check-removal"}) {
		t.Fatal("absent removal", r, err, a.steps)
	}
	a.check.Exists = true
	r, err = c.Remove(t.Context(), target, account.Email, "old")
	if err != nil || r.Outcome != users.OutcomeAbsent || !r.PreviousRemoved {
		t.Fatal("confirmed removal", r, err)
	}
	a.writeErr = errors.New("lost add acknowledgement")
	r, err = c.Replace(t.Context(), target, "old", account)
	if !errors.Is(err, a.writeErr) || r.Outcome != users.OutcomeUnknown || !r.PreviousRemoved {
		t.Fatal("partial replacement hidden", r, err)
	}
	a.steps = nil
	a.removeErr = errors.New("lost remove acknowledgement")
	r, err = c.Replace(t.Context(), target, "old", account)
	if !errors.Is(err, a.removeErr) || r.Outcome != users.OutcomeUnknown || r.PreviousRemoved || !slices.Equal(a.steps, []string{"check", "remove"}) {
		t.Fatal("failed remove proceeded to add", r, err, a.steps)
	}
	a.removeErr = nil
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a.afterRemove = cancel
	a.steps = nil
	r, err = c.Replace(ctx, target, "old", account)
	if !errors.Is(err, context.Canceled) || r.Outcome != users.OutcomeAbsent || !r.PreviousRemoved || !slices.Equal(a.steps, []string{"check", "remove"}) {
		t.Fatal("cancellation after removal lost facts", r, err, a.steps)
	}
}

func TestAccountInputsConflictsAndExecutionFence(t *testing.T) {
	c, _, a, target, account := commands(t)
	for _, change := range []func(*users.Account){
		func(a *users.Account) { a.Email = "" },
		func(a *users.Account) { a.Email = strings.Repeat("a", 257) },
		func(a *users.Account) { a.Protocol = 255 },
		func(a *users.Account) { a.Password = "unexpected" },
		func(a *users.Account) { a.Email = string([]byte{0xff}) },
		func(a *users.Account) {
			a.Protocol = users.Trojan
			a.ID = [16]byte{}
			a.Password = string([]byte{0xff})
		},
		func(a *users.Account) { a.Protocol = users.Trojan },
		func(a *users.Account) {
			a.Protocol = users.Trojan
			a.ID = [16]byte{}
			a.Password = strings.Repeat("x", 4097)
		},
	} {
		bad := account
		change(&bad)
		if _, err := c.Create(t.Context(), target, bad); !errors.Is(err, users.ErrInvalid) {
			t.Fatal("invalid account reached native adapter", err)
		}
	}
	if _, err := c.Remove(t.Context(), target, "", "old"); !errors.Is(err, users.ErrInvalid) {
		t.Fatal("empty remove", err)
	}
	stale := target
	stale.Generation++
	if _, err := c.Create(t.Context(), stale, account); !errors.Is(err, instance.ErrExecutionConflict) {
		t.Fatal("stale mutation", err)
	}
	if len(a.steps) != 0 {
		t.Fatal("invalid input executed checks", a.steps)
	}
	a.checkErr = users.ErrConflict
	if _, err := c.Create(t.Context(), target, account); !errors.Is(err, users.ErrConflict) || len(a.steps) != 1 {
		t.Fatal("collision proceeded to write", err, a.steps)
	}
}

func TestAccountMutationDeadlineAndStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, life, a, target, account := commands(t)
		a.block = true
		if _, err := c.Create(t.Context(), target, account); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("unbounded mutation", err)
		}
		done := make(chan error, 1)
		go func() { _, err := c.Create(t.Context(), target, account); done <- err }()
		synctest.Wait()
		if _, err := c.Remove(t.Context(), target, account.Email, "old"); !errors.Is(err, instance.ErrBusy) {
			t.Fatal("mutation overlap", err)
		}
		if _, err := life.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal("Stop did not cancel/join mutation", err)
		}
	})
}

func TestAccountMutationsRejectStaleRevision(t *testing.T) {
	for _, operation := range []string{"replace", "remove"} {
		t.Run(operation, func(t *testing.T) {
			c, _, a, target, account := commands(t)
			a.check = users.AccountCheck{Exists: true, Revision: "newer", DesiredRevision: "desired"}
			var err error
			if operation == "replace" {
				_, err = c.Replace(t.Context(), target, "old", account)
			} else {
				_, err = c.Remove(t.Context(), target, account.Email, "old")
			}
			if !errors.Is(err, users.ErrConflict) || len(a.steps) != 1 {
				t.Fatal("stale request mutated a newer account", err, a.steps)
			}
		})
	}
}

func TestAccountCapacityDoesNotHideMissingReplacement(t *testing.T) {
	c, _, a, target, account := commands(t)
	a.check.AtCapacity = true
	if _, err := c.Create(t.Context(), target, account); !errors.Is(err, users.ErrLimit) || len(a.steps) != 1 {
		t.Fatal("create exceeded observed capacity", err, a.steps)
	}
	a.steps = nil
	if _, err := c.Replace(t.Context(), target, "old", account); !errors.Is(err, users.ErrNotFound) || len(a.steps) != 1 {
		t.Fatal("missing replacement lost its contract at capacity", err, a.steps)
	}
}

func TestStopDuringReplacementPreservesConfirmedRemoval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, life, a, target, account := commands(t)
		a.check.Exists = true
		a.blockAdd = true
		type outcome struct {
			result users.MutationResult
			err    error
		}
		done := make(chan outcome, 1)
		go func() { r, err := c.Replace(t.Context(), target, "old", account); done <- outcome{r, err} }()
		synctest.Wait()
		if _, err := life.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		got := <-done
		if !errors.Is(got.err, context.Canceled) || got.result.Outcome != users.OutcomeUnknown || !got.result.PreviousRemoved {
			t.Fatal("cancelled write lost its effects", got)
		}
	})
}
