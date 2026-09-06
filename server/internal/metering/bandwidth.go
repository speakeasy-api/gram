package metering

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
)

const (
	mcpBandwidthPublishTimeout = 10 * time.Second
	mcpBandwidthSource         = "mcp_bandwidth_middleware"
)

// MCPServerType is a stable classification of a resolved MCP runtime server.
type MCPServerType string

const (
	MCPServerTypeHosted          MCPServerType = "hosted"
	MCPServerTypeRemote          MCPServerType = "remote"
	MCPServerTypeTunneled        MCPServerType = "tunneled"
	MCPServerTypeMeta            MCPServerType = "meta"
	MCPServerTypeDirectToolset   MCPServerType = "direct_toolset"
	MCPServerTypePlatformToolset MCPServerType = "platform_toolset"
)

type mcpBandwidthContextKey struct{}

type mcpBandwidthExchange struct {
	mu               sync.Mutex
	scope            Scope
	serverType       MCPServerType
	serverID         string
	serverSlug       string
	attributed       bool
	serverAttributed bool
	ingress          atomic.Int64
	egress           atomic.Int64
}

// AttributeMCPBandwidth assigns an MCP HTTP exchange to a resolved project.
// It is a no-op outside NewMCPBandwidthMiddleware and keeps the first trusted
// attribution if a handler crosses multiple internal dispatch layers.
func AttributeMCPBandwidth(ctx context.Context, organizationID string, projectID uuid.UUID) {
	exchange, ok := ctx.Value(mcpBandwidthContextKey{}).(*mcpBandwidthExchange)
	if !ok || exchange == nil {
		return
	}

	exchange.mu.Lock()
	defer exchange.mu.Unlock()
	if exchange.attributed {
		return
	}
	exchange.scope = ProjectScope(organizationID, projectID)
	exchange.attributed = true
}

// AttributeMCPBandwidthServer assigns resolved server provenance to an MCP HTTP
// exchange. The server ID's namespace is determined by serverType: mcp_servers
// for hosted, remote, and tunneled servers; meta_mcp_servers for meta servers;
// toolsets for direct toolsets; and the stable slug for platform toolsets.
// serverSlug is the canonical or stable route slug for the resolved server.
func AttributeMCPBandwidthServer(ctx context.Context, serverType MCPServerType, serverID, serverSlug string) {
	exchange, ok := ctx.Value(mcpBandwidthContextKey{}).(*mcpBandwidthExchange)
	if !ok || exchange == nil || serverType == "" || serverID == "" {
		return
	}

	exchange.mu.Lock()
	defer exchange.mu.Unlock()
	if exchange.serverAttributed {
		return
	}
	exchange.serverType = serverType
	exchange.serverID = serverID
	exchange.serverSlug = serverSlug
	exchange.serverAttributed = true
}

// NewMCPBandwidthMiddleware meters application-visible request and response
// body bytes for MCP and Platform MCP runtime routes. It deliberately ignores
// headers, HTTP framing, unread request bytes, and nested gateway fan-out.
//
// Ordering invariant: install this inside recovery and after middleware that
// enriches the request context without consuming request bodies or transforming
// successful response bodies. Panics and outer-middleware rejections are
// intentionally outside the billable exchange.
func NewMCPBandwidthMiddleware(logger *slog.Logger, publisher gcp.Publisher[*meteringv1.MeterReading]) func(http.Handler) http.Handler {
	logger = logger.With(attr.SlogComponent("mcp-bandwidth-meter"))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestPath := r.URL.Path
			if !isMCPRuntimePath(requestPath) {
				next.ServeHTTP(w, r)
				return
			}

			customDomain := ""
			if domainCtx := customdomains.FromContext(r.Context()); domainCtx != nil {
				customDomain = domainCtx.Domain
			}

			exchange := new(mcpBandwidthExchange)
			if r.Body != nil {
				r.Body = &countingReadCloser{ReadCloser: r.Body, bytes: &exchange.ingress}
			}
			meteredWriter := &countingResponseWriter{ResponseWriter: w, bytes: &exchange.egress}
			ctx := context.WithValue(r.Context(), mcpBandwidthContextKey{}, exchange)
			next.ServeHTTP(meteredWriter, r.WithContext(ctx))

			if isHTMLResponse(meteredWriter.Header()) {
				// Browser-facing install pages share runtime route shapes but are
				// product UI, not MCP traffic, so their bodies are not billable.
				return
			}

			exchange.mu.Lock()
			scope := exchange.scope
			attributed := exchange.attributed
			serverType := exchange.serverType
			serverID := exchange.serverID
			serverSlug := exchange.serverSlug
			serverAttributed := exchange.serverAttributed
			exchange.mu.Unlock()
			if !attributed {
				return
			}

			occurredAt := time.Now().UTC()
			operationID := uuid.NewString()
			attributes := map[string]string{AttributeRequestPath: requestPath}
			if customDomain != "" {
				attributes[AttributeCustomDomain] = customDomain
			}
			if serverAttributed {
				attributes[AttributeMCPServerType] = string(serverType)
				attributes[AttributeMCPServerID] = serverID
				if serverSlug != "" {
					attributes[AttributeMCPServerSlug] = serverSlug
				}
			}
			readings := make([]Reading, 0, 2)
			for _, measured := range []struct {
				definition Definition
				value      int64
			}{
				{definition: MCPBandwidthIngress(), value: exchange.ingress.Load()},
				{definition: MCPBandwidthEgress(), value: exchange.egress.Load()},
			} {
				if measured.value == 0 {
					continue
				}
				reading, err := NewUsage(UsageInput{
					Meter:       measured.definition,
					Scope:       scope,
					OperationID: operationID,
					Value:       measured.value,
					OccurredAt:  occurredAt,
					ProducedAt:  occurredAt,
					Source:      mcpBandwidthSource,
					Attributes:  attributes,
				})
				if err != nil {
					logger.ErrorContext(ctx, "build MCP bandwidth reading", attr.SlogError(err))
					return
				}
				readings = append(readings, reading)
			}
			if len(readings) == 0 {
				return
			}

			publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mcpBandwidthPublishTimeout)
			defer cancel()
			if err := publishReadings(publishCtx, publisher, readings); err != nil {
				logger.ErrorContext(publishCtx, "publish MCP bandwidth readings", attr.SlogError(err))
			}
		})
	}
}

func publishReadings(ctx context.Context, publisher gcp.Publisher[*meteringv1.MeterReading], readings []Reading) error {
	results := make([]gcp.PublishResult, len(readings))
	for i, reading := range readings {
		results[i] = publisher.Publish(ctx, toProto(reading, reading.ID()))
	}

	var errs []error
	for i, result := range results {
		if _, err := result.Get(ctx); err != nil {
			errs = append(errs, fmt.Errorf("publish meter reading %d: %w", i, err))
		}
	}
	return errors.Join(errs...)
}

func isMCPRuntimePath(path string) bool {
	for _, prefix := range [...]string{"/mcp/", "/platform/mcp/"} {
		if remainder, ok := strings.CutPrefix(path, prefix); ok {
			return remainder != "" && !strings.ContainsRune(remainder, '/')
		}
	}
	return false
}

func isHTMLResponse(header http.Header) bool {
	mediaType, _, err := mime.ParseMediaType(header.Get("Content-Type"))
	return err == nil && (mediaType == "text/html" || mediaType == "application/xhtml+xml")
}

type countingReadCloser struct {
	io.ReadCloser
	bytes *atomic.Int64
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.bytes.Add(int64(n))
	return n, err //nolint:wrapcheck // An io.Reader wrapper must preserve sentinel error identity.
}

type countingResponseWriter struct {
	http.ResponseWriter
	bytes *atomic.Int64
}

func (w *countingResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.bytes.Add(int64(n))
	return n, err //nolint:wrapcheck // A ResponseWriter wrapper must preserve the underlying write error.
}

// Flush preserves streaming through response-writer wrapper chains.
func (w *countingResponseWriter) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (w *countingResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
