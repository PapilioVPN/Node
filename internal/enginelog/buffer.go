// Package enginelog owns bounded, sensitive engine-output replay and live reads.
package enginelog

import (
	"errors"
	"math"
	"strings"
	"sync"
	"time"
)

// ErrClosed means the buffer or subscription no longer accepts work.
var ErrClosed = errors.New("engine log reader or buffer closed")

// ErrReaders means all configured reader slots are occupied.
var ErrReaders = errors.New("engine log reader limit reached")

// ErrCursor means a cursor refers to records not yet observed by this buffer.
var ErrCursor = errors.New("engine log cursor is ahead of the buffer")

// Limits bounds retained entries, bytes per entry and admitted subscriptions.
type Limits struct {
	Records   int
	LineBytes int
	Readers   int
}

// Execution identifies one native run within its owning binding.
type Execution struct {
	PID       int
	StartedAt time.Time
}

// Source identifies the observed OS output stream, independently of severity.
type Source uint8

const (
	SourceUnknown Source = iota
	Stdout
	Stderr
)

// Level is native severity; Unknown includes access lines without a severity.
type Level uint8

const (
	Unknown Level = iota
	Debug
	Info
	Warning
	Error
)

// Record is sensitive raw output. Source is the OS stream; Level is a recognized
// native severity or Unknown. Text excludes the line delimiter. Append assigns
// Sequence and ObservedAt; other fields describe the producer's actual execution.
type Record struct {
	Sequence   uint64
	Execution  Execution
	ObservedAt time.Time
	Source     Source
	Level      Level
	Text       string
	Truncated  bool
}

// Delivery reports the exact retention gap preceding Record for this reader.
type Delivery struct {
	Record  Record
	Skipped uint64
}

// Buffer is a concurrency-safe binding-scoped ring. The composition owns Close.
type Buffer struct {
	mu      sync.Mutex
	limits  Limits
	records []Record
	last    uint64
	readers int
	closed  bool
	wake    chan struct{}
}

// New requires positive limits and a bounded total retention budget.
func New(limits Limits) (*Buffer, error) {
	if limits.Records <= 0 || limits.Records > 100000 ||
		limits.LineBytes <= 0 || limits.LineBytes > 65536 ||
		limits.Readers <= 0 || limits.Readers > 256 ||
		limits.Records > (64<<20)/limits.LineBytes {
		return nil, errors.New("engine log limits require 1..100000 records, 1..65536 bytes, 1..256 readers and at most 64 MiB text")
	}
	return &Buffer{limits: limits, records: make([]Record, limits.Records)}, nil
}

// Limits returns the immutable retention and admission bounds.
func (b *Buffer) Limits() Limits { return b.limits }

// Append assigns observation order without waiting for consumers to drain or
// performing I/O. Text is copied into bounded owned storage.
func (b *Buffer) Append(record Record) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrClosed
	}
	if b.last == math.MaxUint64 {
		b.closed = true
		b.signal()
		return ErrClosed
	}
	if len(record.Text) > b.limits.LineBytes {
		record.Text = record.Text[:b.limits.LineBytes]
		record.Truncated = true
	}
	record.Text = strings.Clone(record.Text)
	b.last++
	record.Sequence = b.last
	record.ObservedAt = time.Now().UTC()
	b.records[(b.last-1)%uint64(len(b.records))] = record
	b.signal()
	return nil
}

// Recent returns up to limit latest records, oldest first, as an independent
// slice. It remains available after Close. Text strings are immutable.
func (b *Buffer) Recent(limit int) ([]Record, error) {
	if limit <= 0 || limit > b.limits.Records {
		return nil, errors.New("engine log tail limit outside retention bound")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	count := min(uint64(limit), b.last)
	records := make([]Record, int(count))
	for i := range records {
		records[i] = b.records[(b.last-count+uint64(i))%uint64(len(b.records))]
	}
	return records, nil
}

// Subscribe starts strictly after a binding-local cursor; zero replays all
// retained entries and reports any earlier loss. Future cursors are rejected.
// Call Close to release the reader slot. No background goroutine is created.
func (b *Buffer) Subscribe(after uint64) (*Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if after > b.last {
		return nil, ErrCursor
	}
	if b.readers == b.limits.Readers {
		return nil, ErrReaders
	}
	b.readers++
	return &Subscription{buffer: b, cursor: after}, nil
}

// Close stops publication and wakes readers. Buffered records remain readable.
func (b *Buffer) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.signal()
}

// signal is called with mu held. A notification exists only while readers wait.
func (b *Buffer) signal() {
	if b.wake != nil {
		close(b.wake)
		b.wake = nil
	}
}
