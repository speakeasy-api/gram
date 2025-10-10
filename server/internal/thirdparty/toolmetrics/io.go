package toolmetrics

import (
	"fmt"
	"io"
)

// countingReadCloser wraps a ReadCloser and counts bytes as they're read
type countingReadCloser struct {
	io.ReadCloser
	count   int
	onClose func(int)
}

// NewCountingReadCloser creates a new countingReadCloser that counts bytes as they're read
func NewCountingReadCloser(rc io.ReadCloser, onClose func(int)) io.ReadCloser {
	return &countingReadCloser{
		ReadCloser: rc,
		count:      0,
		onClose:    onClose,
	}
}

func (crc *countingReadCloser) Read(p []byte) (n int, err error) {
	n, err = crc.ReadCloser.Read(p)
	crc.count += n
	return n, fmt.Errorf("error reading: %w", err)
}

func (crc *countingReadCloser) Close() error {
	if crc.onClose != nil {
		crc.onClose(crc.count)
	}

	err := crc.ReadCloser.Close()
	if err != nil {
		return fmt.Errorf("error closing: %w", err)
	}

	return nil
}
