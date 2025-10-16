package toolmetrics

import (
	"fmt"
	"io"
)

// countingReadCloser wraps an io.ReadCloser and counts the total bytes read.
type countingReadCloser struct {
	io.ReadCloser
	count   int
	onClose func(int)
}

// NewCountingReadCloser creates a new countingReadCloser that counts bytes as they're read.
func NewCountingReadCloser(rc io.ReadCloser, onClose func(int)) io.ReadCloser {
	return &countingReadCloser{
		ReadCloser: rc,
		onClose:    onClose,
		count:      0,
	}
}

// Read implements io.ReadCloser. The error is not wrapped
// wrapcheck also doesn't support wrapping io.EOF: https://github.com/tomarrell/wrapcheck/issues/39
// io.EOF is treated as a sentinel value and must not be wrapped: https://github.com/golang/go/issues/39155
func (crc *countingReadCloser) Read(p []byte) (n int, err error) {
	n, err = crc.ReadCloser.Read(p)
	crc.count += n
	return n, err //nolint:wrapcheck // a lot of Go code in the wild still relies on `err == io.EOF` instead of `errors.Is(err, io.EOF)` so we should avoid wrapping.
}

func (crc *countingReadCloser) Close() error {
	err := crc.ReadCloser.Close()
	if crc.onClose != nil {
		crc.onClose(crc.count)
	}
	if err != nil {
		return fmt.Errorf("close counting reader: %w", err)
	}
	return nil
}
