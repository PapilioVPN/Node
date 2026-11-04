package xray

import (
	"bytes"
	"strings"
	"testing"
)

func TestDiagnosticTailAcrossWriteBoundaries(t *testing.T) {
	for _, chunks := range [][]string{
		{}, {""}, {"abc"}, {"12345678"}, {"123456789"},
		{"123", "456", "789", "ab"}, {"12345678", "9", "ab"},
		{"a", "123456789", "bc"}, {"12345678901234567890", ""},
	} {
		t.Run(strings.Join(chunks, "/"), func(t *testing.T) {
			tail := diagnosticTail{data: make([]byte, 8)}
			var all []byte
			for _, chunk := range chunks {
				n, err := tail.Write([]byte(chunk))
				if n != len(chunk) || err != nil {
					t.Fatalf("write = %d, %v", n, err)
				}
				all = append(all, chunk...)
				want := all[max(0, len(all)-8):]
				if got := tail.bytes(); !bytes.Equal(got, want) || tail.truncated != (len(all) > 8) {
					t.Fatalf("tail = %q, truncated=%v; want %q", got, tail.truncated, want)
				}
			}
		})
	}
}
