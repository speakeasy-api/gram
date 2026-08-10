package providers

import (
	"fmt"
	"io"
)

// ReadBoundedBody reads at most limit bytes and fails loudly when the cap is
// hit, so an oversized vendor response surfaces as a size error instead of a
// baffling JSON decode failure on a silently truncated body. Every provider
// bounds its own response reads with this (guardian bounds dialing and
// redirects, not body sizes).
func ReadBoundedBody(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response body exceeded the %d-byte limit", limit)
	}
	return body, nil
}
