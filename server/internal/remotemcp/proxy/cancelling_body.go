package proxy

import (
	"context"
	"fmt"
	"io"
	"time"
)

// cancellingBody wraps a non-streaming upstream response body and cancels
// the associated forward context when the body is closed. timer (when
// non-nil) is the phase timer that bounds the body-read window; stopping
// it on close avoids leaving an AfterFunc goroutine alive after the body
// is drained. Used only for non-streaming responses; streamReader plays
// the equivalent role for streaming responses.
type cancellingBody struct {
	io.ReadCloser

	cancel context.CancelFunc
	timer  *time.Timer
}

func (c *cancellingBody) Close() error {
	if c.timer != nil {
		c.timer.Stop()
	}

	err := c.ReadCloser.Close()
	c.cancel()

	if err != nil {
		return fmt.Errorf("close upstream response body: %w", err)
	}

	return nil
}
