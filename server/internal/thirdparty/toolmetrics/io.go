package toolmetrics

import (
	"io"
)

// countingReadCloser wraps a ReadCloser and counts bytes as they're read
type countingReadCloser struct {
	io.ReadCloser
	count   uint64
	onClose func(uint64)
}

// NewCountingReadCloser creates a new countingReadCloser that counts bytes as they're read
func NewCountingReadCloser(rc io.ReadCloser, onClose func(uint64)) io.ReadCloser {
	return &countingReadCloser{
		ReadCloser: rc,
		count:      0,
		onClose:    onClose,
	}
}

func (crc *countingReadCloser) Read(p []byte) (n int, err error) {
	n, err = crc.ReadCloser.Read(p)
	crc.count += uint64(n) //nolint:gosec // byte counts from io.Read are safe to convert
	return n, err          //nolint:wrapcheck // passthrough wrapper
}

func (crc *countingReadCloser) Close() error {
	if crc.onClose != nil {
		crc.onClose(crc.count)
	}
	return crc.ReadCloser.Close() //nolint:wrapcheck // passthrough wrapper
}
