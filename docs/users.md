# Live users

`internal/users.Service` reads native account metadata and `users.Commands`
creates, replaces and removes single accounts on one managed engine binding.
The tested adapter uses Xray **v26.10.31** (`a4a3dff`) on Linux amd64/arm64 and
its private `HandlerService`. Native client/server tests cover VMess AEAD,
ordinary VLESS and Trojan over isolated local TCP, plus VLESS Vision over verified
TLS 1.3 with an inner HTTPS payload. These are specific
account/traffic checks, not complete protocol/transport support. No user
management CLI command or external management endpoint is provided yet.

## Composition and ownership

Construct `xray.NewUserReader(replyBytes, timeout)` and inject it with the existing
`instance.Service` into `users.New(instance, reader, maxUsers)`. The adapter reuses
the session's private gRPC connection; queries do not create subprocesses or new
clients/connections. The native generated client remains in the adapter package.
Required transitive modules are pinned in `go.mod/go.sum`; the official native
client's source revision remains unchanged.

Use `Target{Generation, Inbound}` from the corresponding instance's current
snapshot. Generations are local to that Service lifetime, change on each startup
attempt and are not durable identities or authorization credentials. Transport
authentication, instance membership and per-inbound authorization must still be
applied at the management boundary. One binding's generation cannot authorize
access to another binding.

Each query or command acquires one `instance.Access`. This pins the recorded ready native
session and supplies a context cancelled by the caller or Stop. Its owner joins
the operation and closes access on every return. One access is admitted per instance;
contended access/start/restart/configuration replacement returns `ErrBusy` without
a queue. Status remains available without native I/O. This excludes conflicting
Node-owned operations; it is not a lock on unrelated privileged native clients.

Stop cancels access, shuts down the owned engine and joins access closure before
admitting another execution. Cancelling a Stop waiter ends only its wait; another
Stop can join. Cancelling a read releases its access after the query returns and
does not stop an otherwise healthy engine. A stale generation returns
`ErrExecutionConflict` before any RPC. Failed/stopped executions return
`ErrNotReady`; native query failures do not trigger restart or rewrite lifecycle
health facts.

## Query contracts

| Operation | Native work | Result |
| --- | --- | --- |
| `Get(ctx, target, email)` | One `GetInboundUsers` with an explicit nonempty email selector. | Native email/level metadata and an opaque revision, or `ErrNotFound`. Never uses a list-and-filter path. |
| `Count(ctx, target)` | One `GetInboundUsersCount`. | Native signed 64-bit count; negative/missing responses fail rather than becoming zero. No account collection is fetched. |
| `List(ctx, target, limit)` | One native count preflight, then one complete `GetInboundUsers` response if the count fits. | All observed metadata, ordered by email then level, or an error. No silently truncated result, cursor or fake page. |

Inbound tags and lookup identifiers require **1..256 bytes**; they are not trimmed
or interpreted as RFC email addresses. Lookup case behavior belongs to the native
engine and returned spelling is preserved. An empty Get identifier is rejected
before it could become native fetch-all. Lists preserve native empty identifiers
and duplicate metadata when present; they do not invent identifiers or deduplicate
accounts. Account levels retain native `uint32` values.

Configure `maxUsers` in **1..100,000** and request `1 <= limit <= maxUsers`. The
reader requires a positive timeout and **1 byte..64 MiB** native response bound.
The timeout covers the entire List, not a fresh deadline for each RPC. Every
response has the gRPC receive-byte limit. Count overflow rejects List before
fetch-all; a post-response item check also rejects growth beyond the requested
limit. These are serialized-message-byte/item limits, not a claim that decoded protobuf
memory equals serialized size.

The pinned native API has no pagination, user-metadata-only response or atomic
count-plus-read operation. A bounded whole response is therefore an explicit
fallback. Concurrent external native mutations may change count/membership
between the two calls; the returned list describes the fetched response, not a
transactional snapshot. List uses two RPCs regardless of user count, without a
per-user read. Count avoids response materialization, but the engine's own
counting algorithm may still scan its account index.

Returned fields are **email, level and an opaque revision**. Native Get/List replies can contain
credentials; they stay on the private native boundary and are discarded during
mapping. Metadata itself can be sensitive and untrusted; consumers own disclosure
and display/encoding. Do not automatically log it. `UserQueryError` excludes raw
native text from normal errors while preserving the original gRPC cause through
`Unwrap` for explicit inspection. `ErrLimit` covers item overflow and native byte
exhaustion; incompatible session types return `ErrUnsupported`. Unknown or
non-user inbounds return actual native failures, never a successful empty/zero
result or a string-matched invented classification.

Results are actual queries, without a second user index, freshness cache or
periodic poller. The caller knows their target generation; a later restart may
already have happened when it processes a completed result. Query results do not
assert continuous readiness or survive a new Node/Service lifetime.

## Single-account mutations

Inject one `xray.UserAccounts` as `IAccountInspector` and `IAccountWriter` into
`users.NewCommands(instance, inspector, writer, timeout)`. The constructor requires
a positive whole-command timeout. Generated requests, account payloads and native
credential indexes stay inside Xray; commands own the check/write ordering.
Neither constructor performs I/O. The adapter reuses the session's existing
connection and creates no retry worker, subprocess or user cache.

`Account` is sensitive desired input: valid UTF-8, nonempty **1..256-byte** `Email`, native
`uint32` `Level`, and a `Protocol` selecting VMess/VLESS/Trojan. VMess/VLESS use a
**16-byte** `ID` and no password; Trojan uses a valid UTF-8 **1..4096-byte** `Password` and no
ID. VLESS also accepts `FlowNone` (empty) or `FlowVision`
(`xtls-rprx-vision`). Nonempty flow on other protocols, unknown flows and the
client-only `-udp443` suffix are invalid before native access. VLESS
encryption/reverse/test settings are not part of this input; replacing an existing
account with those settings fails rather than erasing them. Account values must
never be logged or copied into read results.

| Operation | Contract |
| --- | --- |
| `Create(ctx, target, account)` | Add an absent identity. An equal account is unchanged; a different existing account conflicts. |
| `Replace(ctx, target, expected, account)` | Require the observed account revision before a changed write. Missing identity fails; equal desired input is unchanged, including replay after a lost acknowledgement. |
| `Remove(ctx, target, email, expected)` | Require the observed revision for an existing identity. Absent identity succeeds without a write. Removal revokes new authentication, while established streams continue. |

Read `User.Revision` with Get/List, or retain `MutationResult.Revision` after a
successful create/replace/no-op. Replace/Remove require a nonempty token of at
most 128 bytes. Revisions use HMAC-SHA256 over the native user representation with
a random private key owned by that session. They change with credentials/level/flow
and session replacement, without exposing a password-checking hash. Treat tokens
as sensitive metadata, not authorization. They are content fences, not durable
sequence numbers: identical content recreated within the same execution has the
same revision. No atomicity with unrelated privileged native clients is promised.

Every changed command checks actual inbound type, identity and credential aliases
before writing. VMess command keys, VLESS UUID authentication masking and Trojan
password hashes follow the pinned native implementation. Duplicate identity or
a credential shared with another email returns `ErrConflict`, including removal
that could revoke a sibling account. Identifier case follows the native index;
exact desired replay preserves the existing native email spelling. A stale
revision cannot overwrite/remove different newer content within that execution.

The pinned API has no targeted inbound-type lookup, credential-index lookup,
conditional mutation or atomic account replacement. Inspection therefore uses
one bounded `ListInbounds`, one user-count preflight and one whole account reply.
Its reply data stays inside the adapter. Configure `UserAccountOptions` explicitly:
`ReplyBytes` **1 byte..64 MiB**, `MaxInbounds` **1..10,000**, `MaxUsers`
**1..100,000**, and positive `Timeout`. Both collection counts are checked after
receipt; count preflight rejects oversized users before fetch-all. Create also
rejects growth past `MaxUsers`; removal at capacity remains possible. Pre-existing
state beyond the selected limits fails safely. Native response byte bounds do
not claim identical decoded-memory bounds. The command deadline spans all reads
and writes, in addition to the adapter's inspection/RPC deadlines.

Inspection makes three RPCs regardless of user count. A changed create/remove
adds one direct `AlterInbound`; replacement adds remove then add. An unchanged
request sends no mutation. There is no per-user RPC, list-and-filter single read,
native bulk claim or hidden rollback. Inspection scans native credentials once;
only the selected current record and desired account receive revision tokens.

### VLESS flow and secure transport

Flow is explicit per live account. Empty means ordinary VLESS, even if the source
inbound's `settings.flow` defaults static clients to Vision. Live commands do not
inherit or override input from that source default. Static clients still follow
the native configuration builder on startup/restart. A flow-only change uses
the same conditional remove/add operation, changes the revision and leaves
sibling accounts alone. Flow does not create a different UUID authentication
identity: credential aliases conflict across flows.

For TCP, the pinned native Vision implementation requires direct TLS 1.3 or
REALITY. The executed secure acceptance here covers **raw TCP + TLS 1.3**, with
CA/name verification and no insecure verification override. It does not establish
REALITY, UDP, multiplexing or every transport as supported. Configure stream
security separately in the captured Xray configuration. A native AddUser
acknowledgement confirms an account change, not that its stream settings or a
remote client can carry traffic. Incompatible transport settings remain a real
connection failure; commands do not silently downgrade Vision.

Native tests round-trip a 196,608-byte inner HTTPS payload through owned Xray
client/server processes, verify TLS 1.3 at the destination and compare exact
bytes in both directions. They check both flow transitions, replay, stale
revision/generation, removal, sibling isolation and restart restoring the static
source while dropping live accounts. Wrong flow, untrusted CA, wrong server name,
Vision on TLS 1.2 and Vision on an unencrypted transport cannot reach the actual
target. Ordinary VLESS succeeds under the corresponding transport controls.
Certificates are ephemeral test data; no system trust or host routing is changed.
These checks establish behavior, not a measured Vision splice/throughput claim.

### Effects, interruption and recovery

`MutationResult.Outcome` reports this request's acknowledged effects:

| Outcome | Meaning |
| --- | --- |
| `unchanged` | No mutation was sent by this request. This can accompany a validation/conflict/read failure. |
| `applied` | The requested account add/replacement was acknowledged. `Revision` is its token. |
| `absent` | Absence was observed, removal was acknowledged, or replacement stopped after confirmed removal before add. |
| `unknown` | A mutation returned an error without a reliable acknowledgement. The native effect may have happened. |

`PreviousRemoved` records a confirmed old removal even when replacement's add
fails. Error and effect are separate: cancellation after a received acknowledgement
can return an error with a known applied/absent outcome. No automatic retry or
rollback occurs. After unknown effects, first establish that native work has finished, then read
actual state. If completion cannot be established after a transport failure,
stop/restart and join the owned execution before recovery. A read alone cannot
fence a late native write. Once work is settled, identical Create/Replace can
be unchanged and repeated removal can observe absence. A
replacement whose removal happened but add did not needs explicit Create recovery;
Replace returns `ErrNotFound` for the missing identity. Node access serializes its
checks and effects, but does not lock unrelated native clients or fence an RPC
still executing remotely after a transport failure.

Live edits do not update captured configuration, its desired/applied revisions or
persistent storage. Restart/full configuration replacement reloads captured
source and discards those live changes. Generation/revision tokens must be refreshed.
Removal/credential replacement is not hard session termination: tests verify new
connections with revoked credentials fail while an already authenticated TCP
stream continues. Native errors preserve their cause through `Unwrap` while
`UserMutationError` excludes potentially sensitive text from default formatting.

## Verification and cost

Controlled tests cover native payloads, exact selector/call counts, item/byte
bounds, post-count growth, collision aliases, advanced account protection,
revision fencing, error privacy, cancellation/deadlines, Stop and partial effects.
Native acceptance composes real client/server processes for VMess/VLESS/Trojan:
create, equal replay, credential replacement, old/new denied/accepted traffic,
sibling isolation, stale revision, deletion, stream preservation and restart.
Loopback targets are explicitly allowed only in isolated native test settings;
the pinned engine otherwise blocks private targets for these authenticated inbounds.

Lost-acknowledgement tests inject an error **after the actual native mutation**,
then verify replay/recovery against actual engine state. This demonstrates
application outcome handling, not a reproduction of every transport fault.

See [observation benchmarks](../CONTRIBUTING.md#user-observation-checks) and
[mutation benchmarks](../CONTRIBUTING.md#account-mutation-checks). They measure
control operations against pinned native engines, not forwarding throughput or
comparative performance. Whole-response inspection is O(users + inbounds);
request counts remain bounded independently of user count.
