package xray

// diagnosticTail retains the last bytes without allocating per write. Its owner
// must serialize writes and read only after the process's output copying stops.
type diagnosticTail struct {
	data      []byte
	next      int
	size      int
	truncated bool
}

func (b *diagnosticTail) Write(p []byte) (int, error) {
	n := len(p)
	if n > len(b.data)-b.size {
		b.truncated = true
	}
	if n >= len(b.data) {
		copy(b.data, p[n-len(b.data):])
		b.next, b.size = 0, len(b.data)
	} else {
		copied := copy(b.data[b.next:], p)
		copy(b.data, p[copied:])
		b.next = (b.next + n) % len(b.data)
		b.size = min(len(b.data), b.size+n)
	}
	return n, nil
}

func (b *diagnosticTail) bytes() []byte {
	output := make([]byte, b.size)
	if b.size < len(b.data) {
		copy(output, b.data[:b.size])
	} else {
		n := copy(output, b.data[b.next:])
		copy(output[n:], b.data[:b.next])
	}
	return output
}
