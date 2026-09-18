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
	// Body is the leading part of the response body: Cursor explains a
	// rejected request there, and the status alone does not say why.
	Body string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("cursor usage request failed with status %s", e.Status)
	}
	return fmt.Sprintf("cursor usage request failed with status %s: %s", e.Status, e.Body)
}
