package users

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/papilio-platform/Node/internal/instance"
)

// Protocol selects the account credential family, independently of transport.
type Protocol uint8

const (
	VMess Protocol = iota + 1
	VLESS
	Trojan
)

// MaxPasswordBytes bounds a Trojan credential in bytes.
const MaxPasswordBytes = 4096

// VLESSFlow selects inbound account behavior, independently of stream security.
type VLESSFlow string

const (
	FlowNone   VLESSFlow = ""
	FlowVision VLESSFlow = "xtls-rprx-vision"
)

// ErrConflict rejects a different account, stale revision or authentication alias.
var ErrConflict = errors.New("user account conflict")

// Account is sensitive desired account input. ID belongs to VMess/VLESS;
// Password belongs to Trojan; Flow belongs to VLESS and defaults to no flow.
// Do not log this value or expose it in read results.
type Account struct {
	Email    string
	Level    uint32
	Protocol Protocol
	ID       [16]byte
	Password string
	Flow     VLESSFlow
}

// AccountCheck reports authoritative native identity and full account equality.
// An authentication alias must fail the check with ErrConflict before mutation.
type AccountCheck struct {
	Exists          bool
	Equal           bool
	AtCapacity      bool
	Revision        AccountRevision
	DesiredRevision AccountRevision
}

// RemovalCheck reports the selected native identity without credentials.
type RemovalCheck struct {
	Exists   bool
	Revision AccountRevision
}

// IAccountInspector checks native protocol, bounds and credential collisions.
// Checks and writes share the same borrowed execution access; external native
// clients are outside that serialization contract.
type IAccountInspector interface {
	Check(context.Context, instance.ISession, string, Account) (AccountCheck, error)
	CheckRemoval(context.Context, instance.ISession, string, string) (RemovalCheck, error)
}

// IAccountWriter executes one native mutation, without retries or ownership of
// the borrowed session. An error may follow an effect at the engine.
type IAccountWriter interface {
	Add(context.Context, instance.ISession, string, Account) error
	Remove(context.Context, instance.ISession, string, string) error
}

// MutationOutcome describes acknowledged effects of this request, not durability.
type MutationOutcome string

const (
	OutcomeUnchanged MutationOutcome = "unchanged"
	OutcomeApplied   MutationOutcome = "applied"
	OutcomeAbsent    MutationOutcome = "absent"
	OutcomeUnknown   MutationOutcome = "unknown"
)

// MutationResult retains confirmed removal across a failed replacement. Unknown
// requires settled native work before observation/replay. Live edits are volatile.
type MutationResult struct {
	Outcome         MutationOutcome
	PreviousRemoved bool
	Revision        AccountRevision
}

// Commands coordinates single-account changes on one generation-scoped binding.
type Commands struct {
	access    IAccess
	inspector IAccountInspector
	writer    IAccountWriter
	timeout   time.Duration
}

// NewCommands requires a positive whole-operation timeout and injected owners.
func NewCommands(access IAccess, inspector IAccountInspector, writer IAccountWriter, timeout time.Duration) (*Commands, error) {
	if access == nil || inspector == nil || writer == nil || timeout <= 0 {
		return nil, ErrInvalid
	}
	return &Commands{access: access, inspector: inspector, writer: writer, timeout: timeout}, nil
}

func validAccount(account Account) bool {
	if len(account.Email) == 0 || len(account.Email) > MaxIdentifierBytes || !utf8.ValidString(account.Email) || !utf8.ValidString(account.Password) {
		return false
	}
	switch account.Protocol {
	case VMess:
		return account.Password == "" && account.Flow == FlowNone
	case VLESS:
		return account.Password == "" && (account.Flow == FlowNone || account.Flow == FlowVision)
	case Trojan:
		return account.ID == [16]byte{} && account.Flow == FlowNone && len(account.Password) > 0 && len(account.Password) <= MaxPasswordBytes
	default:
		return false
	}
}
