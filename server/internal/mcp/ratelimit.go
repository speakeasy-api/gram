package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

// checkCustomerRateLimit checks the customer-configured rate limit for a toolset.
// It reads the rate_limit_rpm from the already-loaded toolset struct (no extra DB query).
// On rate limiter errors, it fails open (logs warning and allows the request).
//
// Returns (true, nil) when the rate limit is exceeded and the JSON-RPC error
// response has already been written to w. The caller should return nil to stop
// further processing without triggering the oops error handler.
// Returns (false, nil) when the request is allowed to proceed.
// Returns (false, error) only on unexpected write failures.
func (s *Service) checkCustomerRateLimit(ctx context.Context, w http.ResponseWriter, rateLimitRPM pgtype.Int4, key string) (bool, error) {
	if s.rateLimiter == nil {
		return false, nil
	}

	if !rateLimitRPM.Valid || rateLimitRPM.Int32 <= 0 {
		return false, nil
	}

	rpm := int(rateLimitRPM.Int32)

	result, err := s.rateLimiter.Allow(ctx, "customer:"+key, rpm)
	if err != nil {
		// Fail open: log warning but allow the request through.
		s.logger.WarnContext(ctx, "customer rate limit check failed",
			attr.SlogError(err),
			slog.String("mcp_key", key),
		)
		return false, nil
	}

	ratelimit.SetHeaders(w, result)

	s.metrics.RecordRateLimitCheck(ctx, "customer", key, result.Allowed)

	if !result.Allowed {
		if writeErr := writeJSONRPCRateLimitError(w, rpm, result); writeErr != nil {
			return true, writeErr
		}
		return true, nil
	}

	return false, nil
}

// rateLimitErrorData is the structured data attached to a JSON-RPC rate limit error.
type rateLimitErrorData struct {
	RetryAfterMs int64  `json:"retryAfterMs"`
	Limit        int    `json:"limit"`
	Window       string `json:"window"`
}

// writeJSONRPCRateLimitError writes a JSON-RPC error response for a rate-limited
// request. The response uses HTTP 200 (not 429) to maintain MCP protocol
// compatibility, since this is a JSON-RPC-level error, not an HTTP-level error.
func writeJSONRPCRateLimitError(w http.ResponseWriter, limit int, result ratelimit.Result) error {
	retryAfterMs := max(time.Until(result.ResetAt).Milliseconds(), 1000)

	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      nil,
		"error": map[string]any{
			"code":    rateLimitExceeded,
			"message": fmt.Sprintf("Rate limit exceeded. This MCP server is limited to %d requests per minute.", limit),
			"data": rateLimitErrorData{
				RetryAfterMs: retryAfterMs,
				Limit:        limit,
				Window:       "1m",
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal rate limit error: %w", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, writeErr := w.Write(body)
	if writeErr != nil {
		return fmt.Errorf("write rate limit error response: %w", writeErr)
	}

	return nil
}
