# Bounded engine logs

`internal/enginelog.Buffer` provides a recent tail and live, resumable reads for
one identified engine binding. The Xray launcher captures native console output
only when its composition explicitly supplies `LaunchOptions.Logs`. There is no
log command or management endpoint yet. The executed composition is in the
[native log acceptance test](../tests/integration/xray_logs_linux_test.go).

## Ownership and disclosure

Create one buffer with explicit `Limits`, inject that same buffer into the
binding's launcher and give authorized consumers its read operations. Keep it
across native restarts. Join all owned engine sessions before closing the buffer;
close each subscription when its consumer leaves. The lifecycle service and its
`ISession` contract do not acquire log-management responsibilities.

Raw engine output is sensitive and untrusted. It can contain addresses, account
identifiers, filesystem paths, credentials or terminal control bytes. This is an
explicit internal capability, not a redaction or authorization boundary. Do not
automatically send records to `slog`, ordinary errors or unauthenticated clients.
Consumers own safe disclosure and display/encoding. Native validation and version
subprocess diagnostics remain separate private error details; they are not
published as runtime log records.

## Read and retention contract

| Operation | Result and limits |
| --- | --- |
| `New(Limits)` | Positive record, line-byte and reader bounds. Maximums: 100,000 records, 65,536 bytes per record, 256 subscriptions and 64 MiB of retained text capacity. Record metadata, two bounded native framing prefixes and consumer snapshots are additional memory. |
| `Recent(limit)` | Independent slice of up to `limit` newest retained records, oldest first. Require `1 <= limit <= Records`. Available after closure; an empty buffer returns an empty slice. |
| `Subscribe(after)` | Reader starting strictly after a sequence from this buffer. Zero replays retained history. Reject future cursors with `ErrCursor` and exhausted admission with `ErrReaders`. Closed buffers still allow bounded replay subscriptions. |
| `Next(ctx)` | One `Delivery`, containing a record and the exact number of skipped sequences immediately before it. Waits for new output when caught up. Cancellation preserves the cursor and ends only this call. |
| Subscription `Close()` | Releases the reader slot, wakes pending reads with `ErrClosed` and is idempotent. Concurrent `Next` calls consume distinct entries; their callers must coordinate processing order if needed. |
| Buffer `Close()` | Rejects further append, wakes readers and preserves the retained tail. Readers drain remaining records then receive `io.EOF`. Idempotent; it neither stops engines nor closes caller-owned subscriptions. |

Retention is a count/byte bound, without disk persistence or a time guarantee.
Oldest records are overwritten. Readers share this ring and have no individual
queues or background goroutines. If publication overtakes a reader during replay
or live consumption, its next delivery explicitly reports the retention gap.
Readers cannot block publication while waiting on their own I/O. Buffer operations
share a short memory-only mutex; copying a maximum-size tail still has a bounded
cost. Caller-owned goroutines must join their pending reads.

Sequences start at one and increase at append under the buffer lock. The
observation timestamp is Node's UTC append time, not a parsed native event time.
Save the last **processed** sequence to resume; using an older cursor intentionally
replays records. A cursor belongs only to this in-memory buffer and binding. A
new Node/buffer lifetime resets it; no durable or cross-binding replay is promised.
The buffer closes rather than wrapping an exhausted 64-bit sequence.

`Record.Execution` contains native PID and startup time, distinguishing executions
within the binding across restart. A stopped execution does not close the binding's
buffer. Use [lifecycle observations](instances.md) for actual exit and cleanup;
absence of new records is not health or proof of exit.

## Xray capture contract

- Capture runtime stdout and stderr separately. `Source` is the OS stream, not
  severity. Known native `[Debug]`, `[Info]`, `[Warning]` and `[Error]` prefixes
  (with the native timestamp when present) map to `Level`. Other lines, including
  access records without a level and plain stderr, remain `Unknown`.
- Preserve the original engine configuration. File destinations and `none`
  remain authoritative. Files are not tailed or silently redirected; console
  bootstrap messages may still appear before Xray installs its configured logger.
  Native/upstream logging may itself drop messages before console delivery; Node
  sequence gaps measure only records already observed by this buffer.
- Frame LF and CRLF, preserve empty lines and flush an unterminated final line
  after native `Wait` joins output copying. Retain only the configured byte prefix
  of an oversized line, set `Truncated`, discard its remainder and continue
  draining subsequent lines. Limits count bytes; text may be non-UTF-8 or end
  inside a character. Consumers must choose an explicit encoding/display policy.
- Ordering is Node's append order. Within each stream it follows completed lines;
  there is no original total ordering across OS pipes. Unterminated fragments are
  published at process completion, stdout then stderr. Original native timestamps
  remain in raw text without being substituted for observation time.
- Failed runtime startup flushes captured output before returning, even without a
  usable session. Failures before runtime execution publish no runtime records.
  Default error strings still exclude sensitive diagnostics. Closing capture
  early suppresses more records but never interrupts pipe draining or cleanup.

## Verification and cost

Unit tests cover tail ownership/order, explicit gaps during replay and live
consumption, reader admission, cancellation, closure and concurrent publication.
Framing tests cover split/oversized/empty/partial lines, stream/severity identity,
continued draining and unchanged private diagnostic retention. CI fuzzes chunk
boundaries and arbitrary raw bytes with bounded input and workers.

Pinned Xray **v26.10.31** acceptance on Linux amd64/arm64 verifies native startup
replay, live logs for actual loopback HTTP traffic, cursor continuity across real
restart, recorded run identity, post-stop drain/replay and preserved file/disabled
logging. Controlled OS tests additionally exercise bounded output on failed
startup without leaking it into normal errors.

The [runtime-output benchmark](../CONTRIBUTING.md#engine-log-checks) measures the
actual Node framing/retention/read boundary with 128 native-format lines per
write and zero/two draining readers. It includes diagnostic retention and native
severity recognition. It excludes engine execution, pipe scheduling and downstream
network/display costs. It establishes neither native traffic throughput nor a
performance advantage over another project. Text is copied into bounded owned
storage to avoid retaining oversized producer allocations; no pool, unsafe
conversion or per-reader copying pipeline is used.
