# Live user observations

`internal/users.Service` reads native account metadata for one managed engine
binding. The tested adapter uses Xray **v26.10.31** (`a4a3dff`) on Linux amd64/arm64
and the private native `HandlerService`. The
[native acceptance composition](../tests/integration/users_linux_test.go) verifies
reads from real VMess, VLESS and Trojan inbounds. This establishes metadata reads,
not account mutations, synchronization or protocol traffic support. No user
management command or external management endpoint is provided yet.

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

Each query acquires one `instance.Access`. This pins the recorded ready native
session and supplies a context cancelled by the caller or Stop. Its owner joins
the query and closes access on every return. One access is admitted per instance;
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
| `Get(ctx, target, email)` | One `GetInboundUsers` with an explicit nonempty email selector. | Exact native email/level metadata, or `ErrNotFound`. Never uses a list-and-filter path. |
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

Returned fields are **email and level only**. Native Get/List replies can contain
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

## Verification and cost

Controlled tests cover exact selector/call counts, item and byte limits, response
growth after count, missing users, native error privacy, timeouts, cancellations,
unsupported sessions and simultaneous Stop. Lifecycle tests cover execution
fencing, conflicting admission, cancelled waiters and joined cleanup. Native
acceptance verifies all three metadata operations on actual VMess/VLESS/Trojan
accounts, empty/missing/non-user inbounds, actual gRPC response-byte rejection,
and stale/new reads across native restart.

The [native query benchmark](../CONTRIBUTING.md#user-observation-checks) measures
the application/access/adapter/RPC path on a ready engine with two VMess accounts.
Get and Count issue one RPC each; List uses two. This is control-query cost, not
forwarding throughput, account-write coverage or comparative speed evidence.
