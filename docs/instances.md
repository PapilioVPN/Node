# Instance lifecycle and observation

`internal/instance.Service` coordinates one identified managed engine process and one captured
configuration revision. Its constructor performs no engine I/O. The tested
composition uses the native [Xray launcher](engines/xray.md) on Linux amd64/arm64.
There is no management endpoint or engine CLI command in this revision.

## Operations

- `Start(ctx)` starts the bound configuration and commits `AppliedRevision` only
  after native API readiness and successful admission. An already-ready start
  probes/reuses the same session, with at most one admitted probe. Starts that
  overlap startup, shutdown or another reuse probe return `ErrBusy` without engine
  work or queuing. Failed instances require explicit stop/recovery;
  observation never restarts an engine automatically.
- `Status()` reads recorded facts without subprocesses, RPCs or background polling.
  `ReadyAt` is the last successful API response, not the time of this status read.
  `ObservedAt` timestamps the returned snapshot. These are not continuous health
  guarantees or traffic observations.
- `Stop(ctx)` interrupts startup or owns one shutdown operation. Its context bounds
  caller waiting; accepted cleanup continues and another stop can join it. No
  application state lock is held across engine work.

A context already cancelled at admission prevents both start and stop from doing
engine work. Cancellation during startup joins cleanup before returning; a session
returned after interruption cannot commit its revision. Once admitted, a successful
session outlives the start request. Cancelling a reuse probe ends that call without
stopping a healthy engine or recording a lifecycle failure.

Shutdown cancels an admitted reuse probe and joins both that probe and native
cleanup before admitting another run. Probe cancellation uses the adapter's
bounded request path; it does not create a background worker per caller. A late
probe result returns a conflict with its underlying cause and cannot change the
shutdown outcome. Concurrent stop waiters share one cleanup operation and its
native error; cancelling one waiter does not cancel that owned cleanup.

Operation results contain a current snapshot, not a durable operation receipt.
Another caller may have started a later run when a completed stop waiter returns.
The stop error describes the joined cleanup; use the snapshot's phase and process
facts to interpret current state. These internal synchronous operations do not
provide a durable replay key, an automatic retry or configuration replacement.

## State meaning

| Phase | Meaning |
| --- | --- |
| `stopped` | No execution was started, or owned cleanup succeeded. A previous abnormal exit can still be reported as historical failure. |
| `starting` | Native startup is in progress; the desired revision is not yet committed. Process identity/version can remain unavailable until startup returns. |
| `ready` | The native API responded and no exit has been observed. API freshness is described by `ReadyAt`. |
| `stopping` | Accepted interruption or shutdown is waiting for owned cleanup. |
| `failed` | Startup, readiness or cleanup failed, or a running engine exited unexpectedly. Even exit code zero is unexpected without a stop request. |

Snapshots include identity, desired/applied revisions, native version and its
observation time, process PID, startup/readiness times, confirmed exit details and
cleanup outcome. The PID is historical after exit. A nil `Exit` means termination
has not been observed, not proof that the OS process is alive. Cleanup completion
and success are separate: `CleanupComplete` records a finished attempt and
`CleanupFailure` records release failure.

`AppliedRevision` identifies the last successfully admitted configuration; exit
does not erase it. Failed/cancelled starts cannot claim it. Desired revisions are
opaque engine-provided identifiers; Xray hashes captured bytes and preparation
policy, excluding ephemeral runtime paths. It does not hash referenced file
contents or claim atomicity with external changes.

Errors preserve native causes and `ErrBusy`, `ErrFailed`, `ErrUnexpectedExit` or
`ErrCleanupIncomplete` classification. Error objects are shared read-only; callers
map them explicitly at a transport boundary. A successful cleanup can leave a
`stopped` snapshot alongside an abnormal-exit error; this confirms released
resources and does not turn that exit into graceful success. Startup cleanup
failures remain `failed`, including when no usable session was returned.

## Verification and cost

Controlled tests use Go's `testing/synctest` to verify contended admission, late
completions, cancelled waiters, independent instances, revision history and engine
call counts. A burst of 32 starts during an admitted probe causes no extra native
requests; concurrent stops share one cleanup owner. Real pinned-engine tests
compose this service with Xray, query native
readiness, stop the process and inject SIGKILL into only the owned test child.
They verify actual exit causes, reaping, cleanup and retained applied revisions.
They also interrupt application admission after actual native readiness, join
concurrent stops, confirm process/socket cleanup and explicitly start a new run.
Status polling does not issue per-user or per-read engine requests. The
[status benchmark](../CONTRIBUTING.md#native-xray-tests) measures this memory-only
operation over a real session; it does not establish data-plane throughput or a
performance advantage over another implementation.
