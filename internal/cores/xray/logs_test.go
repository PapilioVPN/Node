package xray

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/papilio-platform/Node/internal/enginelog"
)

func TestRuntimeLogFramingAndDiagnostics(t *testing.T) {
	b, _ := enginelog.New(enginelog.Limits{Records: 10, LineBytes: 8, Readers: 1})
	defer b.Close()
	tail := &diagnosticTail{data: make([]byte, 12)}
	o := newRuntimeOutput(tail, b)
	o.execution = enginelog.Execution{PID: 42}
	for _, chunk := range []string{"one\r", "\n\n12345678", "\r\n", strings.Repeat("x", 100000), "\nlast"} {
		if n, err := o.stdout.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatal(n, err)
		}
	}
	o.stderr.Write([]byte("error\n"))
	o.flush()
	records, _ := b.Recent(10)
	want := []string{"one", "", "12345678", "xxxxxxxx", "error", "last"}
	if len(records) != len(want) {
		t.Fatalf("framing: %+v", records)
	}
	for i, r := range records {
		if r.Text != want[i] || r.Truncated != (i == 3) || r.Execution.PID != 42 || r.ObservedAt.IsZero() {
			t.Fatalf("record %d: %+v", i, r)
		}
	}
	if records[4].Source != enginelog.Stderr || records[5].Source != enginelog.Stdout {
		t.Fatal("lost stream source")
	}
	if got := string(tail.bytes()); got != "x\nlasterror\n" || !tail.truncated {
		t.Fatalf("private tail no longer retained: %q", got)
	}
}

func TestNativeLogSeverityIsNotStreamSeverity(t *testing.T) {
	b, _ := enginelog.New(enginelog.Limits{Records: 8, LineBytes: 256, Readers: 1})
	defer b.Close()
	o := newRuntimeOutput(&diagnosticTail{data: make([]byte, 32)}, b)
	for _, line := range []string{"2026/11/04 12:00:00.123456 [Debug] detail", "2026/11/04 12:00:00 [Info] detail", "[Warning] detail", "[Error] detail", "from source accepted tcp:target email: test [Error] detail", "unrecognized native output"} {
		o.stdout.Write([]byte(line + "\n"))
	}
	o.stderr.Write([]byte("plain stderr\n"))
	o.flush()
	records, _ := b.Recent(8)
	if len(records) != 7 {
		t.Fatalf("missing output: %+v", records)
	}
	for i, want := range []enginelog.Level{enginelog.Debug, enginelog.Info, enginelog.Warning, enginelog.Error, enginelog.Unknown, enginelog.Unknown, enginelog.Unknown} {
		if records[i].Level != want {
			t.Fatalf("invented severity at %d: %+v", i, records[i])
		}
	}
}

func FuzzRuntimeLogFraming(f *testing.F) {
	for _, seed := range []string{"", "a\nb\r\nlast", "\n\n", strings.Repeat("x", 1000) + "\nvalid\n", "12345678\r\n", "raw\x00\xff\n"} {
		f.Add([]byte(seed), uint8(3))
	}
	f.Fuzz(func(t *testing.T, input []byte, split uint8) {
		if len(input) > 65536 {
			t.Skip()
		}
		b, _ := enginelog.New(enginelog.Limits{Records: 128, LineBytes: 8, Readers: 1})
		defer b.Close()
		o := newRuntimeOutput(&diagnosticTail{data: make([]byte, 32)}, b)
		step := int(split) + 1
		for i := 0; i < len(input); i += step {
			o.stdout.Write(input[i:min(i+step, len(input))])
		}
		o.flush()
		parts := bytes.Split(input, []byte{'\n'})
		if len(parts[len(parts)-1]) == 0 {
			parts = parts[:len(parts)-1]
		}
		parts = parts[max(0, len(parts)-128):]
		records, _ := b.Recent(128)
		if len(records) != len(parts) {
			t.Fatalf("line count %d != %d", len(records), len(parts))
		}
		for i, p := range parts {
			if i < len(parts)-1 || len(input) > 0 && input[len(input)-1] == '\n' {
				p = bytes.TrimSuffix(p, []byte{'\r'})
			}
			if records[i].Text != string(p[:min(len(p), 8)]) || records[i].Truncated != (len(p) > 8) {
				t.Fatalf("chunk-dependent framing %d: %+v", i, records[i])
			}
		}
	})
}

func TestLogCaptureClosureStillDrainsOutput(t *testing.T) {
	b, _ := enginelog.New(enginelog.Limits{Records: 1, LineBytes: 8, Readers: 1})
	b.Close()
	tail := &diagnosticTail{data: make([]byte, 8)}
	o := newRuntimeOutput(tail, b)
	input := []byte(strings.Repeat("sensitive\n", 10000) + "last")
	if n, err := o.stderr.Write(input); err != nil || n != len(input) {
		t.Fatal("capture closure stopped native output", n, err)
	}
	o.flush()
	if string(tail.bytes()) != "ive\nlast" {
		t.Fatalf("closed capture lost diagnostic tail: %q", tail.bytes())
	}
}

func TestConcurrentRuntimeStreamsPreserveLines(t *testing.T) {
	logs, _ := enginelog.New(enginelog.Limits{Records: 400, LineBytes: 64, Readers: 1})
	defer logs.Close()
	o := newRuntimeOutput(&diagnosticTail{data: make([]byte, 32)}, logs)
	var writers sync.WaitGroup
	writers.Go(func() {
		for range 200 {
			o.stdout.Write([]byte("[Info] stdout\n"))
		}
	})
	writers.Go(func() {
		for range 200 {
			o.stderr.Write([]byte("[Warning] stderr\n"))
		}
	})
	writers.Wait()
	o.flush()
	records, _ := logs.Recent(400)
	if len(records) != 400 {
		t.Fatalf("missing concurrent output: %d", len(records))
	}
	var stdout, stderr int
	for _, r := range records {
		switch r.Source {
		case enginelog.Stdout:
			stdout++
			if r.Text != "[Info] stdout" || r.Level != enginelog.Info {
				t.Fatal("corrupted stdout", r)
			}
		case enginelog.Stderr:
			stderr++
			if r.Text != "[Warning] stderr" || r.Level != enginelog.Warning {
				t.Fatal("corrupted stderr", r)
			}
		default:
			t.Fatal("lost OS source")
		}
	}
	if stdout != 200 || stderr != 200 {
		t.Fatalf("stream counts: %d %d", stdout, stderr)
	}
}

func BenchmarkRuntimeLogOutput(b *testing.B) {
	for _, readers := range []int{0, 2} {
		b.Run(fmt.Sprintf("readers=%d", readers), func(b *testing.B) {
			logs, _ := enginelog.New(enginelog.Limits{Records: 256, LineBytes: 1024, Readers: 2})
			defer logs.Close()
			o := newRuntimeOutput(&diagnosticTail{data: make([]byte, 4096)}, logs)
			var subs []*enginelog.Subscription
			for range readers {
				s, _ := logs.Subscribe(0)
				defer s.Close()
				subs = append(subs, s)
			}
			line := []byte("2026/11/04 12:00:00.123456 [Info] app/dispatcher: default route for tcp:127.0.0.1:8080\n")
			input := bytes.Repeat(line, 128)
			b.SetBytes(int64(len(input)))
			b.ReportAllocs()
			for b.Loop() {
				o.stdout.Write(input)
				for _, s := range subs {
					for range 128 {
						if _, err := s.Next(b.Context()); err != nil {
							b.Fatal(err)
						}
					}
				}
			}
			o.flush()
		})
	}
}
