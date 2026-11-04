# Xray native integration

The internal adapter validates configuration and owns an Xray **v26.10.31**
(commit `a4a3dff`) process. Native CI runs validation, API readiness and shutdown
checks on **Linux amd64 and arm64**, using checksum-pinned official release
archives. The [instance service](../instances.md) owns application lifecycle
policy. A private HTTP forwarding fixture verifies basic payload traversal across
lifecycle cycles. User management, traffic-statistics operations and control
endpoints are not provided yet; the fixture is not full protocol support evidence.

The official native API client is pinned to the same source revision. Its module
requires Go 1.27; Node builds use Go **1.27.3 or newer**.

## Execution contract

`internal/cores/xray.Checker` invokes the selected absolute executable directly:

```text
xray run -test -format=json -c stdin:
```

The caller supplies a bounded configuration byte slice and retains it unchanged
until the call returns. Xray owns schema and value validation. No second engine
schema, shell command, configuration file write or automatic retry is involved.
Successful native validation does not start listeners or establish readiness.
It can access files and other resources named in the configuration under the
process's privileges; it is not a sandbox.

The constructor performs no I/O. Options require absolute executable and working
directory paths, positive input/output byte limits, an execution timeout and a
pipe wait delay. An optional explicit asset directory must also be absolute.
Relative engine paths resolve from the supplied working directory. The child
inherits no environment; only the optional `XRAY_LOCATION_ASSET` is passed.
Xray's own system asset fallback still exists. No asset-dependent configuration
is covered by the current fixtures.

## Limits and failures

- Oversized input fails before execution with `ErrConfigTooLarge`.
- Each call owns one validation process and waits for it. Cancellation or the
  configured timeout kills this short-lived child and preserves the context cause.
  Pipe draining is bounded separately by `WaitDelay`.
- Output retention is a byte tail capped by `MaxDiagnosticBytes`, including
  unterminated lines. `CheckError` exposes the tail and a truncation flag explicitly.
  Treat this output as sensitive; the default error text excludes it.
- Native exit and launch errors remain inspectable with `errors.As`/`errors.Is`.
  Invalid configurations exit 23 with the tested engine. Missing or non-executable
  binaries remain execution failures, rather than invalid configuration results.
  A configuration can validate and still fail during startup: the tested occupied
  listener case exits 255. Closing the native client after exit interrupts pending
  readiness, preserving the actual process failure rather than waiting for the
  readiness deadline. Releasing the conflict permits explicit stop/start recovery.
- Concurrent calls retain independent process/output state. The calling capability
  owns admission limits; the checker creates no background queue or retry workers.

Run the [native test instructions](../../CONTRIBUTING.md#native-xray-tests) to verify
acceptance, rejection, ambient-configuration isolation and absence of listeners.
Controlled child-process tests separately cover limits, sensitive diagnostics,
cancellation, reaping and concurrent calls.

## Owned process contract

`NewLauncher` captures a bounded copy of the supplied JSON without I/O. The caller
provides the process lifetime context, native check options, an absolute runtime
parent directory and positive readiness/shutdown deadlines. The parent and working
directories remain caller-owned. Callers coordinate conflicting launches.

Startup reads the selected executable's `version` once with the check timeout,
pipe-wait and diagnostic-byte bounds. It does not substitute the linked API
client's version. The executable must not be replaced during startup; the
version observation is not an atomic filesystem snapshot. The session retains
the version and its observation time. Status reads never start another command.

`Revision` is SHA256 of the original configuration bytes and the API preparation
policy identifier. Runtime/socket paths are excluded. Whitespace changes produce
a different revision; referenced file/asset contents and executable versions are
not part of this configuration digest.

Each `Start` creates a fresh 0700 directory with a 0600 API overlay and private
filesystem Unix socket. The original configuration stays unchanged on stdin.
The overlay preserves the native API tag and service order, adds `StatsService`
if absent and replaces the listener with the owned socket. An absent/null API
gets the tag `papilio-api`; an existing empty tag remains invalid. No TCP
management listener is introduced. Native check and run consume the same two
configuration sources. Only strict JSON input is accepted for the projection.

One reusable official gRPC client calls `GetSysStats` over the private socket to
establish API readiness. There is no PID/log-only readiness, per-probe subprocess,
background statistics polling or application-level RPC retry. A failed or cancelled
start joins owned cleanup before returning. After a successful start, its request
context no longer controls the engine; the injected process lifetime does.

`Stop` requests SIGTERM, with native `os/exec` escalation to Kill after `StopTimeout`.
The same delay bounds inherited I/O pipes. Cancelling a stop caller only ends its
wait; shutdown continues under the session owner. `Wait` joins actual exit, client
closure and owned-directory removal. Repeated stops join that same completion.
The session must be stopped or its lifetime cancelled, then waited for.

An abnormal exit, forced kill or cleanup failure remains an inspectable
`ProcessError`; its bounded sensitive diagnostics are excluded from default error
text. API readiness does **not** guarantee that upstream has installed its signal
handlers: the pinned Xray starts its API before GC and SIGTERM subscription.
An early SIGTERM can therefore terminate it without graceful engine cleanup;
Node reports that native exit and still reaps and removes its owned resources.
It does not report the signal exit as graceful success or add a timing-based sleep.
Application state/recovery policy must account for the actual exit outcome.

`Session.Observe` copies confirmed process facts under a short lock: PID,
executable version, startup/readiness times, exit code/signal/cause and cleanup
outcome. Exit becomes visible after the one native Wait, separately from cleanup
completion. `CleanupComplete` means the cleanup attempt finished; a non-nil
`CleanupFailure` means release failed. Returned exit records can be modified
without changing session state; error objects remain shared read-only.
