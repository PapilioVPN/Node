# Xray configuration validation

The internal adapter validates one configuration with Xray **v26.10.31**
(commit `a4a3dff`). Native CI runs the configuration contract on **Linux amd64
and arm64**, using checksum-pinned official release archives. This does not
establish lifecycle, traffic, user management or control API support.

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
- Concurrent calls retain independent process/output state. The calling capability
  owns admission limits; the checker creates no background queue or retry workers.

Run the [native test instructions](../../CONTRIBUTING.md#native-xray-tests) to verify
acceptance, rejection, ambient-configuration isolation and absence of listeners.
Controlled child-process tests separately cover limits, sensitive diagnostics,
cancellation, reaping and concurrent calls.
