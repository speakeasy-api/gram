package usage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	polargo "github.com/polarsource/polar-go"
	polarComponents "github.com/polarsource/polar-go/models/components"
	polarOperations "github.com/polarsource/polar-go/models/operations"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// loggingTransport wraps an http.RoundTripper and logs requests before sending them
type loggingTransport struct {
	transport http.RoundTripper
	logger    *slog.Logger
}

// Useful for debugging Polar requests
func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Log the request details
	t.logger.InfoContext(req.Context(), "outgoing HTTP request to Polar",
		attr.SlogHTTPRequestMethod(req.Method),
		attr.SlogURLFull(req.URL.String()),
		attr.SlogHostName(req.Host),
		attr.SlogHTTPRequestHeaderUserAgent(req.UserAgent()),
	)

	// Log headers (excluding sensitive ones)
	headers := make(map[string]string)
	for key, values := range req.Header {
		if key != "Authorization" && key != "X-API-Key" {
			headers[key] = values[0] // Just log the first value for simplicity
		} else {
			headers[key] = "[REDACTED]"
		}
	}
	t.logger.DebugContext(req.Context(), "request headers", attr.SlogHTTPRequestHeaders(headers))

	// Log request body if present
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			t.logger.ErrorContext(req.Context(), "failed to read request body for logging", attr.SlogError(err))
		} else {
			t.logger.DebugContext(req.Context(), "request body", attr.SlogHTTPRequestBody(string(bodyBytes)))
			// Recreate the body since we consumed it
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}
	}

	// Make the actual request
	resp, err := t.transport.RoundTrip(req)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to perform HTTP request")
	}
	return resp, nil
}

// newLoggingHTTPClient creates an HTTP client with request logging
func newLoggingHTTPClient(logger *slog.Logger, timeout time.Duration) *http.Client {
	transport := &loggingTransport{
		transport: http.DefaultTransport,
		logger:    logger.With(attr.SlogComponent("polar-http-client")),
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}

type PolarClient struct {
	polar  *polargo.Polar
	logger *slog.Logger
}

func NewPolarClient(polar *polargo.Polar, logger *slog.Logger) *PolarClient {
	return &PolarClient{
		polar:  polar,
		logger: logger.With(attr.SlogComponent("polar-usage")),
	}
}

// NewPolarClientWithLogging creates a new Polar client with HTTP request logging
func NewPolarClientWithLogging(polarKey string, logger *slog.Logger) *polargo.Polar {
	httpClient := newLoggingHTTPClient(logger, 30*time.Second)
	return polargo.New(polargo.WithSecurity(polarKey), polargo.WithClient(httpClient), polargo.WithTimeout(30*time.Second))
}

type ToolCallUsageEvent struct {
	OrganizationID   string
	RequestBytes     int64
	OutputBytes      int64
	ToolID           string
	ToolName         string
	ProjectID        string
	ProjectSlug      *string
	OrganizationSlug *string
	ToolsetSlug      *string
	ChatID           *string
	MCPURL           *string
}

func (p *PolarClient) TrackToolCallUsage(ctx context.Context, event ToolCallUsageEvent) {
	if p.polar == nil {
		return
	}

	totalBytes := event.RequestBytes + event.OutputBytes

	metadata := map[string]polarComponents.EventCreateExternalCustomerMetadata{
		"request_bytes": {
			Integer: &event.RequestBytes,
		},
		"output_bytes": {
			Integer: &event.OutputBytes,
		},
		"total_bytes": {
			Integer: &totalBytes,
		},
		"tool_id": {
			Str: &event.ToolID,
		},
		"tool_name": {
			Str: &event.ToolName,
		},
		"project_id": {
			Str: &event.ProjectID,
		},
	}

	if event.ProjectSlug != nil {
		metadata["project_slug"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.ProjectSlug,
		}
	}

	if event.OrganizationSlug != nil {
		metadata["organization_slug"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.OrganizationSlug,
		}
	}

	if event.ToolsetSlug != nil {
		metadata["toolset_slug"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.ToolsetSlug,
		}
	}

	if event.ChatID != nil {
		metadata["chat_id"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.ChatID,
		}
	}

	if event.MCPURL != nil {
		metadata["mcp_url"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.MCPURL,
		}
	}

	_, err := p.polar.Events.Ingest(ctx, polarComponents.EventsIngest{
		Events: []polarComponents.Events{
			{
				Type: polarComponents.EventsTypeEventCreateExternalCustomer,
				EventCreateExternalCustomer: &polarComponents.EventCreateExternalCustomer{
					ExternalCustomerID: event.OrganizationID,
					Name:               "tool-call",
					Metadata:           metadata,
				},
			},
		},
	})

	if err != nil {
		p.logger.ErrorContext(ctx, "failed to ingest usage event to Polar", attr.SlogError(err))
	}
}

type PromptCallUsageEvent struct {
	OrganizationID   string
	RequestBytes     int64
	OutputBytes      int64
	PromptID         *string
	PromptName       string
	ProjectID        string
	ProjectSlug      *string
	OrganizationSlug *string
	ToolsetSlug      *string
	ChatID           *string
	MCPURL           *string
}

func (p *PolarClient) TrackPromptCallUsage(ctx context.Context, event PromptCallUsageEvent) {
	if p.polar == nil {
		return
	}

	totalBytes := event.RequestBytes + event.OutputBytes

	metadata := map[string]polarComponents.EventCreateExternalCustomerMetadata{
		"request_bytes": {
			Integer: &event.RequestBytes,
		},
		"output_bytes": {
			Integer: &event.OutputBytes,
		},
		"total_bytes": {
			Integer: &totalBytes,
		},
		"prompt_name": {
			Str: &event.PromptName,
		},
		"project_id": {
			Str: &event.ProjectID,
		},
	}

	if event.PromptID != nil {
		metadata["prompt_id"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.PromptID,
		}
	}

	if event.ProjectSlug != nil {
		metadata["project_slug"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.ProjectSlug,
		}
	}

	if event.OrganizationSlug != nil {
		metadata["organization_slug"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.OrganizationSlug,
		}
	}

	if event.ToolsetSlug != nil {
		metadata["toolset_slug"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.ToolsetSlug,
		}
	}

	if event.ChatID != nil {
		metadata["chat_id"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.ChatID,
		}
	}

	if event.MCPURL != nil {
		metadata["mcp_url"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.MCPURL,
		}
	}

	_, err := p.polar.Events.Ingest(ctx, polarComponents.EventsIngest{
		Events: []polarComponents.Events{
			{
				Type: polarComponents.EventsTypeEventCreateExternalCustomer,
				EventCreateExternalCustomer: &polarComponents.EventCreateExternalCustomer{
					ExternalCustomerID: event.OrganizationID,
					Name:               "prompt-call",
					Metadata:           metadata,
				},
			},
		},
	})

	if err != nil {
		p.logger.ErrorContext(ctx, "failed to ingest usage event to Polar", attr.SlogError(err))
	}
}

type PlatformUsageEvent struct {
	OrganizationID    string
	PublicMCPServers  int64
	PrivateMCPServers int64
	TotalToolsets     int64
	TotalTools        int64
}

func (p *PolarClient) TrackPlatformUsage(ctx context.Context, event PlatformUsageEvent) {
	if p.polar == nil {
		return
	}

	metadata := map[string]polarComponents.EventCreateExternalCustomerMetadata{
		"public_mcp_servers": {
			Integer: &event.PublicMCPServers,
		},
		"private_mcp_servers": {
			Integer: &event.PrivateMCPServers,
		},
		"total_toolsets": {
			Integer: &event.TotalToolsets,
		},
		"total_tools": {
			Integer: &event.TotalTools,
		},
	}

	_, err := p.polar.Events.Ingest(ctx, polarComponents.EventsIngest{
		Events: []polarComponents.Events{
			{
				Type: polarComponents.EventsTypeEventCreateExternalCustomer,
				EventCreateExternalCustomer: &polarComponents.EventCreateExternalCustomer{
					ExternalCustomerID: event.OrganizationID,
					Name:               "platform-usage",
					Metadata:           metadata,
				},
			},
		},
	})

	if err != nil {
		p.logger.ErrorContext(ctx, "failed to ingest platform usage event to Polar", attr.SlogError(err))
	}
}

const (
	toolCallsMeterID = "7ec16f6d-6189-4262-9898-c64bed7b8a91"
	serversMeterID   = "servers"

	freeTierToolCalls = 1000
	freeTierServers   = 1
)

func (p *PolarClient) GetPeriodUsage(ctx context.Context, orgID string) (*gen.PeriodUsage, error) {
	if p.polar == nil {
		return nil, oops.E(oops.CodeUnexpected, errors.New("polar not initialized"), "Could not get period usage")
	}

	// TODO: Handle the case where the user has a subscription

	customerFilter := polarOperations.CreateMetersQuantitiesQueryParamExternalCustomerIDFilterStr(orgID)

	// For free tier, we need to read the meter directly because the user won't have a subscription
	res, err := p.polar.Meters.Quantities(ctx, polarOperations.MetersQuantitiesRequest{
		ID:                 toolCallsMeterID,
		ExternalCustomerID: &customerFilter,
		StartTimestamp:     time.Now().Add(-1 * time.Hour * 24 * 30),
		EndTimestamp:       time.Now(),
		Interval:           polarComponents.TimeIntervalDay,
	})

	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "Could not get period usage")
	}

	return &gen.PeriodUsage{
		ToolCalls:    int(res.MeterQuantities.Total),
		MaxToolCalls: freeTierToolCalls,
		Servers:      1, // TODO
		MaxServers:   freeTierServers,
	}, nil
}

const (
	gramBusinessProductID = "1361707d-e9c2-4693-91bd-0789fbc09d68"
)

func (p *PolarClient) CreateCheckout(ctx context.Context, orgID string, serverURL string) (string, error) {
	if p.polar == nil {
		return "", oops.E(oops.CodeUnexpected, errors.New("polar not initialized"), "Could not create checkout link")
	}

	res, err := p.polar.Checkouts.Create(ctx, polarComponents.CheckoutCreate{
		ExternalCustomerID: &orgID,
		EmbedOrigin:        &serverURL,
		Products: []string{
			gramBusinessProductID,
		},
	})

	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "Could not create checkout link")
	}

	return res.Checkout.URL, nil
}
