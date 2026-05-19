package otelforwarding

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
)

const (
	// MaxForwardBodyBytes caps the body we are willing to buffer in memory
	// for forwarding. Requests larger than this are still processed by the
	// Goa handler (we let them through with the original body intact) but
	// are skipped for forwarding so we don't OOM on a runaway payload.
	MaxForwardBodyBytes = 4 * 1024 * 1024

	pathPrefix  = "/rpc/hooks.otel"
	pathLogs    = pathPrefix + "/v1/logs"
	pathMetrics = pathPrefix + "/v1/metrics"

	gramKeyHeader = "Gram-Key"
)

// Middleware returns an HTTP middleware that intercepts requests to the
// hooks.otel endpoints, buffers the body, and asynchronously forwards a copy
// to the customer's configured endpoint. The original request continues to
// the downstream Goa handler unchanged so we still process the payload
// ourselves.
//
// Failures (queue full, customer endpoint 5xx, lookup errors) are logged
// only — they never affect the response returned to the caller.
func Middleware(logger *slog.Logger, client *Client, forwarder *Forwarder) func(http.Handler) http.Handler {
	logger = logger.With(attr.SlogComponent("otelforwarding.middleware"))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !shouldIntercept(r) {
				next.ServeHTTP(w, r)
				return
			}

			body, ok := readBodyWithCap(r)
			if !ok {
				// Body exceeded our buffer cap. Skip forwarding but pass the
				// original (unread) body through so the downstream handler
				// still sees it.
				next.ServeHTTP(w, r)
				return
			}

			// Replace r.Body with the buffered copy so the downstream Goa
			// handler can decode it normally.
			r.Body = io.NopCloser(bytes.NewReader(body))

			// Best-effort forward. All errors degrade to a log line — never
			// propagate to the response.
			tryForward(r.Context(), logger, client, forwarder, r, body)

			next.ServeHTTP(w, r)
		})
	}
}

func shouldIntercept(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	path := r.URL.Path
	return path == pathLogs || path == pathMetrics
}

func readBodyWithCap(r *http.Request) ([]byte, bool) {
	if r.Body == nil {
		return nil, true
	}
	// LimitReader returns up to MaxForwardBodyBytes + 1; if we read the
	// full +1 byte we know the body exceeded the cap.
	limited := io.LimitReader(r.Body, MaxForwardBodyBytes+1)
	buf, err := io.ReadAll(limited)
	if err != nil {
		return nil, false
	}
	if len(buf) > MaxForwardBodyBytes {
		// Splice the bytes we already read back onto the rest of the body
		// stream so the downstream handler still gets the full payload.
		r.Body = struct {
			io.Reader
			io.Closer
		}{
			Reader: io.MultiReader(bytes.NewReader(buf), r.Body),
			Closer: r.Body,
		}
		return nil, false
	}
	return buf, true
}

func tryForward(ctx context.Context, logger *slog.Logger, client *Client, forwarder *Forwarder, r *http.Request, body []byte) {
	rawKey := strings.TrimSpace(r.Header.Get(gramKeyHeader))
	if rawKey == "" {
		return
	}

	keyHash, err := auth.GetAPIKeyHash(rawKey)
	if err != nil {
		logger.WarnContext(ctx, "hash gram-key for forwarding lookup", attr.SlogError(err))
		return
	}

	orgID, err := client.repo.GetAPIKeyOrgByKeyHash(ctx, keyHash)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return
	case err != nil:
		logger.WarnContext(ctx, "resolve org from api key for forwarding", attr.SlogError(err))
		return
	}

	cfg, err := client.GetForOrg(ctx, orgID)
	if err != nil {
		logger.WarnContext(ctx, "load otel forwarding config",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		return
	}
	if !cfg.IsConfigured() || !cfg.Enabled {
		return
	}

	subpath := strings.TrimPrefix(r.URL.Path, pathPrefix)
	targetURL, err := url.JoinPath(cfg.URL, subpath)
	if err != nil {
		logger.WarnContext(ctx, "build otel forwarding target url",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		return
	}

	forwarder.Enqueue(ctx, Job{
		OrgID:       orgID,
		URL:         targetURL,
		ContentType: r.Header.Get("Content-Type"),
		Headers:     cfg.Headers,
		Body:        body,
	})
}
