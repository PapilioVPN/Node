package xray

import (
	"bytes"
	"strings"
	"sync"
	"time"

	"github.com/papilio-platform/Node/internal/enginelog"
)

// runtimeOutput serializes diagnostics and both stream framers. Lock ordering is
// output.mu -> Buffer's private mutex; the buffer never calls back into output.
type runtimeOutput struct {
	mu             sync.Mutex
	tail           *diagnosticTail
	logs           *enginelog.Buffer
	execution      enginelog.Execution
	stdout, stderr *logOutput
}

type logOutput struct {
	owner     *runtimeOutput
	source    enginelog.Source
	prefix    []byte
	discarded bool
}

func newRuntimeOutput(tail *diagnosticTail, logs *enginelog.Buffer) *runtimeOutput {
	o := &runtimeOutput{tail: tail, logs: logs}
	o.stdout = &logOutput{owner: o, source: enginelog.Stdout}
	o.stderr = &logOutput{owner: o, source: enginelog.Stderr}
	return o
}

func (w *logOutput) Write(p []byte) (int, error) {
	o := w.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	o.tail.Write(p)
	n := len(p)
	if n > 0 && w.prefix == nil {
		w.prefix = make([]byte, 0, o.logs.Limits().LineBytes+1)
	}
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		part := p
		if i >= 0 {
			part = p[:i]
		}
		room := o.logs.Limits().LineBytes + 1 - len(w.prefix)
		w.prefix = append(w.prefix, part[:min(len(part), room)]...)
		w.discarded = w.discarded || len(part) > room
		if i < 0 {
			break
		}
		w.emit(true)
		p = p[i+1:]
	}
	return n, nil
}

func (w *logOutput) emit(terminated bool) {
	prefix := w.prefix
	if terminated && !w.discarded {
		prefix = bytes.TrimSuffix(prefix, []byte{'\r'})
	}
	limit := w.owner.logs.Limits().LineBytes
	truncated := w.discarded || len(prefix) > limit
	text := string(prefix[:min(len(prefix), limit)])
	// Capture closure must not interrupt pipe draining or lifecycle cleanup.
	_ = w.owner.logs.Append(enginelog.Record{Execution: w.owner.execution, Source: w.source, Level: nativeLogLevel(text), Text: text, Truncated: truncated})
	w.prefix = w.prefix[:0]
	w.discarded = false
}

func (o *runtimeOutput) flush() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, w := range []*logOutput{o.stdout, o.stderr} {
		if len(w.prefix) > 0 || w.discarded {
			w.emit(false)
		}
		w.prefix = nil
	}
}

func nativeLogLevel(text string) enginelog.Level {
	prefix, suffix, ok := strings.Cut(text, "[")
	if !ok {
		return enginelog.Unknown
	}
	if prefix != "" {
		stamp := strings.TrimSuffix(prefix, " ")
		if _, err := time.Parse("2006/01/02 15:04:05.999999", stamp); err != nil {
			return enginelog.Unknown
		}
	}
	switch {
	case strings.HasPrefix(suffix, "Debug] "):
		return enginelog.Debug
	case strings.HasPrefix(suffix, "Info] "):
		return enginelog.Info
	case strings.HasPrefix(suffix, "Warning] "):
		return enginelog.Warning
	case strings.HasPrefix(suffix, "Error] "):
		return enginelog.Error
	default:
		return enginelog.Unknown
	}
}
