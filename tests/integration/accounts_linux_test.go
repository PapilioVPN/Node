package integration_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/cores/xray"
	"github.com/papilio-platform/Node/internal/instance"
	"github.com/papilio-platform/Node/internal/users"
)

// This fault injector drops an acknowledgement after the real native effect.
// It verifies recovery against actual engine state, not a simulated user store.
type lostAccountAck struct {
	writer              users.IAccountWriter
	loseAdd, loseRemove bool
}

var errLostAccountAck = errors.New("injected lost native acknowledgement")

func (w *lostAccountAck) Add(ctx context.Context, session instance.ISession, inbound string, account users.Account) error {
	if err := w.writer.Add(ctx, session, inbound, account); err != nil {
		return err
	}
	if w.loseAdd {
		w.loseAdd = false
		return errLostAccountAck
	}
	return nil
}
func (w *lostAccountAck) Remove(ctx context.Context, session instance.ISession, inbound, email string) error {
	if err := w.writer.Remove(ctx, session, inbound, email); err != nil {
		return err
	}
	if w.loseRemove {
		w.loseRemove = false
		return errLostAccountAck
	}
	return nil
}

func TestAccountLostAcknowledgementsRecoverFromActualState(t *testing.T) {
	life, _ := startAccountServer(t, users.Trojan)
	adapter, err := xray.NewUserAccounts(xray.UserAccountOptions{ReplyBytes: 1 << 20, MaxUsers: 10, MaxInbounds: 10, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	fault := &lostAccountAck{writer: adapter, loseAdd: true}
	commands, err := users.NewCommands(life, adapter, fault, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	target := users.Target{Generation: life.Status().Generation, Inbound: "accounts"}
	account := accountInput(users.Trojan, "recovered", 1)
	readRevision := func() users.AccountRevision {
		t.Helper()
		reader, err := xray.NewUserReader(1<<20, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		queries, err := users.New(life, reader, 10)
		if err != nil {
			t.Fatal(err)
		}
		user, err := queries.Get(t.Context(), target, account.Email)
		if err != nil {
			t.Fatal(err)
		}
		return user.Revision
	}
	result, err := commands.Create(t.Context(), target, account)
	if !errors.Is(err, errLostAccountAck) || result.Outcome != users.OutcomeUnknown {
		t.Fatal("lost create acknowledgement", result, err)
	}
	if r, err := commands.Create(t.Context(), target, account); err != nil || r.Outcome != users.OutcomeUnchanged {
		t.Fatal("create replay duplicated actual user", r, err)
	}
	revision := readRevision()
	account.Password = "isolated-replacement-password"
	fault.loseAdd = true
	result, err = commands.Replace(t.Context(), target, revision, account)
	if !errors.Is(err, errLostAccountAck) || result.Outcome != users.OutcomeUnknown || !result.PreviousRemoved {
		t.Fatal("lost replacement acknowledgement", result, err)
	}
	if r, err := commands.Replace(t.Context(), target, revision, account); err != nil || r.Outcome != users.OutcomeUnchanged {
		t.Fatal("replacement replay changed actual account", r, err)
	}
	revision = readRevision()
	fault.loseRemove = true
	account.Level++
	result, err = commands.Replace(t.Context(), target, revision, account)
	if !errors.Is(err, errLostAccountAck) || result.Outcome != users.OutcomeUnknown || result.PreviousRemoved {
		t.Fatal("lost removal acknowledgement", result, err)
	}
	if _, err := commands.Replace(t.Context(), target, revision, account); !errors.Is(err, users.ErrNotFound) {
		t.Fatal("unknown removal invented rollback", err)
	}
	if r, err := commands.Create(t.Context(), target, account); err != nil || r.Outcome != users.OutcomeApplied {
		t.Fatal("explicit recovery of absent account", r, err)
	}
	revision = readRevision()
	fault.loseRemove = true
	result, err = commands.Remove(t.Context(), target, account.Email, revision)
	if !errors.Is(err, errLostAccountAck) || result.Outcome != users.OutcomeUnknown {
		t.Fatal("lost delete acknowledgement", result, err)
	}
	if r, err := commands.Remove(t.Context(), target, account.Email, revision); err != nil || r.Outcome != users.OutcomeAbsent || r.PreviousRemoved {
		t.Fatal("delete replay against actual absence", r, err)
	}
}

func TestAccountNativeInspectionLimitsDoNotMutate(t *testing.T) {
	life, _ := startAccountServer(t, users.Trojan)
	target := users.Target{Generation: life.Status().Generation, Inbound: "accounts"}
	options := xray.UserAccountOptions{ReplyBytes: 1 << 20, MaxUsers: 1, MaxInbounds: 10, Timeout: time.Second}
	adapter, err := xray.NewUserAccounts(options)
	if err != nil {
		t.Fatal(err)
	}
	commands, err := users.NewCommands(life, adapter, adapter, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	account := accountInput(users.Trojan, "bounded", 1)
	created, err := commands.Create(t.Context(), target, account)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commands.Create(t.Context(), target, accountInput(users.Trojan, "overflow", 2)); !errors.Is(err, users.ErrLimit) {
		t.Fatal("native capacity overflow", err)
	}
	if _, err := commands.Replace(t.Context(), target, created.Revision, accountInput(users.Trojan, "missing", 2)); !errors.Is(err, users.ErrNotFound) {
		t.Fatal("missing replacement at capacity", err)
	}
	wrongFamily := accountInput(users.VLESS, "wrong", 3)
	if _, err := commands.Create(t.Context(), target, wrongFamily); !errors.Is(err, users.ErrUnsupported) {
		t.Fatal("native mismatched account family", err)
	}
	options.ReplyBytes = 32
	tiny, err := xray.NewUserAccounts(options)
	if err != nil {
		t.Fatal(err)
	}
	bounded, err := users.NewCommands(life, tiny, tiny, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bounded.Remove(t.Context(), target, account.Email, created.Revision); !errors.Is(err, users.ErrLimit) {
		t.Fatal("real native observation byte cap", err)
	}
	if r, err := commands.Create(t.Context(), target, account); err != nil || r.Outcome != users.OutcomeUnchanged {
		t.Fatal("rejected operations changed actual account", r, err)
	}
	if _, err := commands.Remove(t.Context(), target, account.Email, created.Revision); err != nil {
		t.Fatal("removal blocked at capacity", err)
	}
}

func BenchmarkAccountNativeMutations(b *testing.B) {
	for _, size := range []int{1, 100} {
		b.Run(fmt.Sprintf("users-%d", size), func(b *testing.B) {
			server, _ := startAccountServer(b, users.Trojan)
			commands := nativeAccountCommands(b, server)
			target := users.Target{Generation: server.Status().Generation, Inbound: "accounts"}
			account := accountInput(users.Trojan, "measured", 1)
			created, err := commands.Create(b.Context(), target, account)
			if err != nil {
				b.Fatal(err)
			}
			revision := created.Revision
			for i := 1; i < size; i++ {
				filler := accountInput(users.Trojan, fmt.Sprintf("filler-%d", i), byte(i+1))
				if _, err := commands.Create(b.Context(), target, filler); err != nil {
					b.Fatal(err)
				}
			}
			b.Run("unchanged", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if r, err := commands.Create(b.Context(), target, account); err != nil || r.Outcome != users.OutcomeUnchanged {
						b.Fatal("unchanged account", err)
					}
				}
			})
			b.Run("replace", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					account.Level++
					result, err := commands.Replace(b.Context(), target, revision, account)
					if err != nil || !result.PreviousRemoved || result.Outcome != users.OutcomeApplied {
						b.Fatal("replacement effects", err)
					}
					revision = result.Revision
				}
			})
		})
	}
}
