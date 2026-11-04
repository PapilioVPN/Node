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
- `Restart(ctx)` explicitly replaces the captured binding from ready, failed or
  stopped state. It joins previous cleanup before starting a fresh execution and
  waits for native readiness. It returns `RestartResult` with previous/current
  snapshots and the previous stop failure, separately from the operation error.

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

### Restart and recovery

One reservation covers both restart phases. `Start` and another `Restart` return
`ErrBusy` during that transition; restart also rejects already-active startup,
shutdown or reuse probes. There is no request queue or implicit health loop.
`Stop` interrupts restart, joins all accepted cleanup and prevents a replacement
from remaining live after stop completes. Cancelling a stop waiter ends only its
wait; another stop can join the same restart completion.

Cancellation during previous shutdown waits for its bounded cleanup and prevents
replacement. Cancellation before replacement startup commits joins candidate
cleanup. A successfully committed replacement outlives the restart request.
This uses the same admitted start/stop paths as standalone lifecycle calls.

`Previous` records the old execution's final stop snapshot. If admission is
rejected, no stop occurs and both snapshots describe the state read at admission.
`Current` records the resulting application state. `StopFailure` preserves the
old native shutdown error even if confirmed cleanup permits successful recovery.
For example, recovering a SIGKILL-terminated engine can return nil operation error,
a ready `Current` and the inspectable SIGKILL cause in `StopFailure`.

Incomplete old cleanup returns `ErrCleanupIncomplete` and blocks a new execution.
Failed replacement returns its actual error with historical `AppliedRevision`;
state is failed, or stopped after an interrupting `Stop` confirms release. The
old process is already gone. No automatic rollback or
retries are attempted. A later explicit restart can recover a released binding.
Cancellation and cleanup failure remain independently inspectable when both occur.
Restart is intentionally disruptive and repeated successful calls each create a
fresh execution; it is not an idempotent retry contract or a live configuration
change. These distinctions apply before any transport maps the result.

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

The tested composition constructs `xray.Launcher` with an explicit process
lifetime, bounded native options and captured JSON, then injects it into
`instance.New(id, "xray", launcher)`. The service owns admission and application
state; the launcher/session own native execution, readiness and cleanup. See the
actual [`nativeInstance` fixture](../tests/integration/instance_acceptance_linux_test.go)
for this composition and its joined shutdown. The executable does not expose
these engine operations as commands or management endpoints yet.

Native acceptance uses Xray **v26.10.31** (`a4a3dff`) on Linux amd64/arm64. The
following checks run against the official checksum-pinned binary, rather than a
mock engine:

| Outcome | Verification |
| --- | --- |
| Usable execution across repeated cycles | Start, native API readiness, unchanged-session reuse, exact local HTTP payload forwarding before/after restart and stop; each replacement/stop confirms child reaping and owned runtime removal. Requests after stop cannot reach the target through the proxy. |
| Request lifetime independence | Cancelling a completed start request leaves its successful session available for probing and real payload forwarding. |
| Native validation rejection | Invalid engine configuration retains exit code 23 and bounded private diagnostics, commits no revision and requires explicit acknowledgement. |
| Runtime startup failure and recovery | An occupied loopback port retains native exit code 255 without waiting for a readiness timeout. Explicit stop, release of the conflict and start recover the same binding. Borrowed listeners/files remain intact. |
| Process owner termination | Ending the injected lifetime reaps the child, closes the native client and removes owned runtime files. Observation records the unrequested exit and keeps applied history; an ended lifetime cannot launch another engine. |
| Unexpected exit and interrupted admission | SIGKILL and concurrent stop tests preserve actual exit causes, reject late application commits and confirm native cleanup before explicit recovery. |
| Explicit restart and interruption | Normal replacement and recovery after SIGKILL retain old termination facts, admit a fresh ready engine and remove the previous run directory. Stop during real replacement admission cleans both executions and prevents resurrection. |

The forwarding fixture uses a private Unix **HTTP proxy** and a loopback HTTP
target, with bounded payloads and client deadlines. It exercises unchanged native
configuration through the lifecycle service. It does not establish full protocol,
account-management, live-reconfiguration or traffic-statistics support. Node
removes its own generated API directory; arbitrary paths referenced by engine
configuration retain their declared engine/operator ownership.

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

The [application lifecycle benchmark](../CONTRIBUTING.md#native-xray-tests) measures
the actual bound-service start/API-ready/stop cycle, including native version
observation and validation. It reports startup/readiness and shutdown/cleanup
separately. Allocation results cover Node/client allocations, not Xray process
memory. A non-graceful native stop fails the measurement; benchmark results must
not silently treat forced termination as successful shutdown.

The restart benchmark measures the actual reserved stop/start operation over a
ready native session. It requires clean old shutdown and fresh readiness; it fails
if the previous stop error is non-nil. Measurements include native version/check/
run/API work, not just the in-memory reservation or payload throughput.
