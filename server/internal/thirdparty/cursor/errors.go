package cursor

import (
	"fmt"
	"time"
)

type RateLimitError struct {
	Status     string
	RetryAfter time.Duration
	Page       int
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("cursor usage request rate limited with status %s; retry after %s", e.Status, e.RetryAfter)
	}
	return fmt.Sprintf("cursor usage request rate limited with status %s", e.Status)
}

type HTTPError struct {
	StatusCode int
	Status     string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("cursor usage request failed with status %s", e.Status)
}
