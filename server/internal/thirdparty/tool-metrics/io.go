package tool_metrics

import (
	"bytes"
	"fmt"
	"io"
)

type capturingReadCloser struct {
	io.ReadCloser
	buffer  *bytes.Buffer
	onClose func()
}

func (c *capturingReadCloser) Read(p []byte) (n int, err error) {
	n, err = c.ReadCloser.Read(p)
	if n > 0 {
		c.buffer.Write(p[:n])
	}
	return
}

func (c *capturingReadCloser) Close() error {
	if c.onClose != nil {
		c.onClose()
	}
	if err := c.ReadCloser.Close(); err != nil {
		return fmt.Errorf("failed to close reader: %w", err)
	}
	return nil
}
