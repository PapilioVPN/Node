package xray

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestCompiledConfigurationReadIsBounded(t *testing.T) {
	for _, size := range []int{0, 15, 16, 17, 1024} {
		input := strings.NewReader(strings.Repeat("x", size))
		data, err := readCompiledBytes(input, 16)
		if size <= 16 {
			if err != nil || !bytes.Equal(data, bytes.Repeat([]byte("x"), size)) {
				t.Fatal("bounded compiled input lost", err)
			}
		} else if !errors.Is(err, ErrConfigTooLarge) || input.Size()-int64(input.Len()) != 17 {
			t.Fatal("compiled input exceeded read bound", err)
		}
	}
}
