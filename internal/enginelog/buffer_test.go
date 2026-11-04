package enginelog_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/papilio-platform/Node/internal/enginelog"
)

func newBuffer(t testing.TB) *enginelog.Buffer {
	t.Helper()
	b, err := enginelog.New(enginelog.Limits{Records: 3, LineBytes: 8, Readers: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return b
}

func appendText(t testing.TB, b *enginelog.Buffer, text string) {
	t.Helper()
	if err := b.Append(enginelog.Record{Text: text}); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionGapAndLiveContinuity(t *testing.T) {
	b := newBuffer(t)
	for _, text := range []string{"one", "two", "three", "four"} {
		appendText(t, b, text)
	}
	s, err := b.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i, want := range []string{"two", "three", "four", "five"} {
		if i == 1 {
			appendText(t, b, "five")
		}
		d, err := s.Next(t.Context())
		wantSkipped := uint64(0)
		if i == 0 {
			wantSkipped = 1
		}
		if err != nil || d.Record.Text != want || d.Record.Sequence != uint64(i+2) || d.Skipped != wantSkipped {
			t.Fatalf("delivery %d: %+v %v", i, d, err)
		}
	}
	for _, text := range []string{"six", "seven", "eight", "nine"} {
		appendText(t, b, text)
	}
	d, err := s.Next(t.Context())
	if err != nil || d.Skipped != 1 || d.Record.Text != "seven" {
		t.Fatalf("slow reader lost unreported records: %+v %v", d, err)
	}
	recent, err := b.Recent(2)
	if err != nil || len(recent) != 2 || recent[0].Text != "eight" || recent[1].Text != "nine" {
		t.Fatalf("tail: %+v %v", recent, err)
	}
	recent[0].Text = "mutated"
	again, _ := b.Recent(2)
	if again[0].Text != "eight" {
		t.Fatal("mutable snapshot escaped")
	}
}

func TestLimitsAndTruncation(t *testing.T) {
	for _, limits := range []enginelog.Limits{{}, {3, 0, 1}, {-1, 8, 1}, {3, 8, 0}, {100001, 8, 1}, {3, 65537, 1}, {3, 8, 257}, {100000, 65536, 1}} {
		if _, err := enginelog.New(limits); err == nil {
			t.Errorf("accepted unbounded/invalid limits: %+v", limits)
		}
	}
	b := newBuffer(t)
	appendText(t, b, "1234567890")
	recent, _ := b.Recent(1)
	if len(recent) != 1 || recent[0].Text != "12345678" || !recent[0].Truncated {
		t.Fatalf("unbounded entry: %+v", recent)
	}
	for _, limit := range []int{-1, 0, 4} {
		if _, err := b.Recent(limit); err == nil {
			t.Errorf("accepted tail limit %d", limit)
		}
	}
	if _, err := b.Subscribe(2); !errors.Is(err, enginelog.ErrCursor) {
		t.Fatalf("future cursor: %v", err)
	}
	s1, _ := b.Subscribe(1)
	defer s1.Close()
	s2, _ := b.Subscribe(1)
	defer s2.Close()
	if _, err := b.Subscribe(1); !errors.Is(err, enginelog.ErrReaders) {
		t.Fatalf("reader bound: %v", err)
	}
	s1.Close()
	s3, err := b.Subscribe(1)
	if err != nil {
		t.Fatal(err)
	}
	s3.Close()
}

func TestCancellationAndClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBuffer(t)
		s, _ := b.Subscribe(0)
		defer s.Close()
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { _, err := s.Next(ctx); done <- err }()
		synctest.Wait()
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("idle cancellation: %v", err)
		}
		appendText(t, b, "retained")
		b.Close()
		b.Close()
		if err := b.Append(enginelog.Record{}); !errors.Is(err, enginelog.ErrClosed) {
			t.Fatalf("publication after close: %v", err)
		}
		d, err := s.Next(t.Context())
		if err != nil || d.Record.Text != "retained" {
			t.Fatalf("closed drain: %+v %v", d, err)
		}
		if _, err := s.Next(t.Context()); !errors.Is(err, io.EOF) {
			t.Fatalf("drained closure: %v", err)
		}
		replay, err := b.Subscribe(0)
		if err != nil {
			t.Fatal(err)
		}
		defer replay.Close()
		if _, err := replay.Next(t.Context()); err != nil {
			t.Fatal(err)
		}
		replay.Close()
		if _, err := replay.Next(t.Context()); !errors.Is(err, enginelog.ErrClosed) {
			t.Fatalf("reader closed: %v", err)
		}
	})
}

func TestCloseInterruptsIdleReader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBuffer(t)
		s, _ := b.Subscribe(0)
		done := make(chan error, 1)
		go func() { _, err := s.Next(t.Context()); done <- err }()
		synctest.Wait()
		s.Close()
		if err := <-done; !errors.Is(err, enginelog.ErrClosed) {
			t.Fatal(err)
		}
		s, _ = b.Subscribe(0)
		defer s.Close()
		go func() { _, err := s.Next(t.Context()); done <- err }()
		synctest.Wait()
		b.Close()
		if err := <-done; !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
	})
}

func TestConcurrentPublicationReadersAndClose(t *testing.T) {
	b := newBuffer(t)
	s, _ := b.Subscribe(0)
	defer s.Close()
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			for range 200 {
				err := b.Append(enginelog.Record{Text: "entry"})
				if err != nil && !errors.Is(err, enginelog.ErrClosed) {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	b.Close()
	var seq uint64
	for {
		d, err := s.Next(t.Context())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if d.Record.Sequence != seq+d.Skipped+1 {
			t.Fatalf("order/gap: %+v after %d", d, seq)
		}
		seq = d.Record.Sequence
	}
	if seq != 400 {
		t.Fatalf("publication count: %d", seq)
	}
}

func TestConcurrentLiveReadersJoinBufferClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBuffer(t)
		var readers sync.WaitGroup
		for range 2 {
			s, err := b.Subscribe(0)
			if err != nil {
				t.Fatal(err)
			}
			readers.Go(func() {
				defer s.Close()
				var last uint64
				for {
					d, err := s.Next(t.Context())
					if errors.Is(err, io.EOF) {
						return
					}
					if err != nil {
						t.Error(err)
						return
					}
					if d.Record.Sequence != last+d.Skipped+1 {
						t.Error("inconsistent live gap")
					}
					last = d.Record.Sequence
				}
			})
		}
		synctest.Wait()
		for range 200 {
			appendText(t, b, "burst")
		}
		b.Close()
		readers.Wait()
		s, err := b.Subscribe(200)
		if err != nil {
			t.Fatal("reader admission not released:", err)
		}
		defer s.Close()
	})
}

func TestCancelledNextPreservesUnreadRecord(t *testing.T) {
	b := newBuffer(t)
	s, _ := b.Subscribe(0)
	defer s.Close()
	appendText(t, b, "one")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	d, err := s.Next(t.Context())
	if err != nil || d.Record.Sequence != 1 {
		t.Fatalf("cancelled read consumed output: %+v %v", d, err)
	}
}

func TestResumeFromProcessedCursor(t *testing.T) {
	b := newBuffer(t)
	if records, err := b.Recent(3); err != nil || len(records) != 0 {
		t.Fatalf("empty tail: %+v %v", records, err)
	}
	for _, text := range []string{"one", "two", "three"} {
		appendText(t, b, text)
	}
	s, err := b.Subscribe(2)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Next(t.Context())
	s.Close()
	if err != nil || d.Record.Sequence != 3 || d.Record.Text != "three" || d.Skipped != 0 {
		t.Fatalf("resume repeated acknowledged output: %+v %v", d, err)
	}
	appendText(t, b, "four")
	resumed, err := b.Subscribe(d.Record.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	next, err := resumed.Next(t.Context())
	if err != nil || next.Record.Sequence != 4 || next.Record.Text != "four" || next.Skipped != 0 {
		t.Fatalf("reconnect lost continuity: %+v %v", next, err)
	}
}
