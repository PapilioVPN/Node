# Engine configuration

`internal/configuration.Service` owns sensitive desired-source reads, bounded
validation and revision-conditional full application for one engine binding.
The [native acceptance composition](../tests/integration/configuration_linux_test.go)
uses Xray v26.10.31 on Linux amd64/arm64. These are internal capabilities; no
configuration command, management endpoint or persistent configuration store is
provided yet. Node settings remain separate from opaque engine source.

## Ownership

Compose one immutable initial candidate, one `instance.Service` using that same
candidate, and one configuration service with an engine-specific `IFactory`.
The factory captures bounded input without I/O. Its candidate supplies original
source, content revision, native validation and the existing `IStarter` contract.
For Xray, use `ConfigurationFactory` with the binding's explicit process lifetime
and `LaunchOptions`; each capture creates a fresh launcher. Keep factory options
immutable, including the executable/environment/resource limits.

Create exactly one configuration service per lifecycle owner and route all
configuration changes through it. The configuration service owns application
orchestration; the instance owns admission, native lifetime and desired/applied
revision facts. Xray owns syntax, hashing, preparation, subprocesses and files.
No engine schema or generated native messages enter the application package.

## Operations

| Operation | Behavior |
| --- | --- |
| `Read()` | Independent copy of original desired bytes with a lifecycle snapshot. Performs no engine I/O. Returns `ErrBusy` while validation/application owns the configuration slot; an out-of-band lifecycle rebind returns `ErrRevisionConflict` rather than mismatched bytes. |
| `Validate(ctx, source)` | Capture and validate effective engine input without changing desired/applied state or stopping the active execution. Returns candidate `Revision`, observed desired `Against` and `RestartRequired` for full activation. This is a point-in-time check, not readiness or a persistent validation receipt. |
| `Apply(ctx, expected, source)` | Reserve lifecycle against the expected desired content revision. A stale revision returns `ErrRevisionConflict` before capture or native work. Probe/reuse a healthy unchanged execution; otherwise validate before joining old cleanup and starting the captured candidate. |

Only one validation/apply operation is admitted per binding. Contention returns
`ErrBusy` without a queue or waiting worker; independent bindings remain
independent. A pre-cancelled operation performs no native work. Inputs are
borrowed until the call returns; the factory copies only after checking the
configured byte bound. `Read` results belong to the caller.

Read bytes and native diagnostic fields are sensitive. They may include
credentials and are not automatically written to logs or normal error strings.
Original input is returned, rather than a reconstructed or redacted runtime dump.
`AppliedRevision` identifies historical successful admission; it does not claim
that an engine is currently healthy or that referenced files still match.

## Application, interruption and recovery

1. Reserve the expected revision through `instance.BeginReplacement` before
   validation. Start/restart/conflicting replacement cannot enter that scope.
   Status remains available; the scope itself does not assert readiness.
2. Capture the source. If unchanged and recorded ready, perform one native probe.
   Success returns `Reused=true` with the same execution, without validation or
   startup subprocesses. Standalone `Validate` still explicitly validates it.
3. Changed, stopped, failed or unsuccessfully probed input goes through native
   validation. Rejection preserves the old binding and any still-running engine;
   an unsuccessful reuse probe remains an actual recorded failure.
4. Join old cleanup. Incomplete cleanup blocks replacement and keeps the old
   desired binding. An abnormal but fully released old run can recover; its stop
   error remains in `StopFailure` even if the new operation succeeds.
5. Bind the candidate after cleanup and a cancellation check, then start it.
   Only actual readiness and admission commit `AppliedRevision`. Failed startup
   retains candidate desired input and the last successfully applied revision.

Xray full replacement is disruptive and requires a new process. This capability
does not promise live routing/outbound changes, preservation of open connections
across replacement, automatic rollback or atomicity across native effects.
Validate can succeed while startup later fails, for example on an occupied port.
Startup rechecks the prepared input because a prior check does not freeze the
executable, assets or other referenced resources. These two checks serve different
effect boundaries; unchanged healthy reuse needs neither.

`Stop` cancels and joins the whole scope, including native preflight and any late
candidate. Cancelling a Stop waiter ends only that wait; cleanup continues and
another Stop can join. Cancelled application joins its accepted cleanup. Before
binding, cancellation keeps old desired input; after binding, candidate desired
input remains available for explicit recovery. A successful admitted execution
outlives its Apply request. Stop during preflight still stops the old execution
after validation joins; it cannot be followed by a late successful replacement.

`ApplyResult` contains previous/current lifecycle snapshots, separate
`StopFailure`, and `Reused`. It is not a durable operation receipt: later lifecycle
calls may already have changed current state. Save the returned desired revision
for a subsequent conditional call. Expected revisions compare captured content,
not a monotonically increasing transaction number; an A -> B -> A change again
matches A. Identical successful retries attach while healthy; repeated failed
input may explicitly attempt recovery. There is no durable replay key or state
surviving a Node process restart.

## Verification and cost

Controlled tests cover native-call counts, stale/oversized/cancelled admission,
unchanged reuse and failed-probe recovery, read ownership, rejected validation,
failed replacement history, contention, validation deadlines and Stop interruption.
The shared lifecycle tests additionally cover late returned sessions, expired Stop
waiters, incomplete cleanup, native failures and fresh desired bindings.

Real Xray tests verify that invalid input preserves the same forwarding process,
unchanged apply keeps its PID, a freedom -> blackhole replacement changes actual
traffic, native validation differs from readiness, listener-conflict failure
retains desired/applied history, explicit recovery works, and owned temporary
directories are removed. CI executes these tests with the race detector on both
supported native architectures.

The [unchanged-apply benchmark](../CONTRIBUTING.md#configuration-checks) measures
capture/hash, scoped admission and a real native readiness probe over one ready
Xray. It checks reuse and unchanged PID. It excludes initial startup and traffic
throughput. This workload supports an implementation before/after comparison;
it does not establish superiority over another Node project.
