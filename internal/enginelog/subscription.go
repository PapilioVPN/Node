package enginelog

import (
	"context"
	"io"
)

// Subscription owns a cursor into its buffer, not a separate queue. Concurrent
// Next calls consume distinct entries. Close is safe concurrently with Next.
type Subscription struct {
	buffer *Buffer
	cursor uint64
	closed bool
}

// Next waits for one record. Cancellation ends only this call. Retention loss
// is explicit; callers may resume after the delivered sequence. io.EOF means the
// closed buffer is drained; ErrClosed means this reader was explicitly closed.
func (s *Subscription) Next(ctx context.Context) (Delivery, error) {
	b := s.buffer
	for {
		if err := ctx.Err(); err != nil {
			return Delivery{}, err
		}
		b.mu.Lock()
		if s.closed {
			b.mu.Unlock()
			return Delivery{}, ErrClosed
		}
		if s.cursor < b.last {
			first := b.last - min(b.last, uint64(len(b.records))) + 1
			next := max(first, s.cursor+1)
			d := Delivery{Record: b.records[(next-1)%uint64(len(b.records))], Skipped: next - s.cursor - 1}
			s.cursor = next
			b.mu.Unlock()
			return d, nil
		}
		if b.closed {
			b.mu.Unlock()
			return Delivery{}, io.EOF
		}
		if b.wake == nil {
			b.wake = make(chan struct{})
		}
		wake := b.wake
		b.mu.Unlock()
		select {
		case <-wake:
		case <-ctx.Done():
			return Delivery{}, ctx.Err()
		}
	}
}

// Close releases admission and interrupts pending Next calls.
func (s *Subscription) Close() {
	b := s.buffer
	b.mu.Lock()
	defer b.mu.Unlock()
	if !s.closed {
		s.closed = true
		b.readers--
		b.signal()
	}
}
