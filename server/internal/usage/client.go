package usage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	polargo "github.com/polarsource/polar-go"
	polarComponents "github.com/polarsource/polar-go/models/components"
	polarOperations "github.com/polarsource/polar-go/models/operations"
	"github.com/redis/go-redis/v9"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/polar"
)

type Client struct {
	polar              *polargo.Polar
	logger             *slog.Logger
	customerStateCache cache.TypedCacheObject[polar.PolarCustomerState]
}

var _ billing.Tracker = (*Client)(nil)
var _ billing.Repository = (*Client)(nil)

func NewClient(polarClient *polargo.Polar, logger *slog.Logger, redisClient *redis.Client) *Client {
	return &Client{
		polar:              polarClient,
		logger:             logger.With(attr.SlogComponent("polar-usage")),
		customerStateCache: cache.NewTypedObjectCache[polar.PolarCustomerState](logger.With(attr.SlogCacheNamespace("polar-customer-state")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
	}
}

func (p *Client) TrackToolCallUsage(ctx context.Context, event billing.ToolCallUsageEvent) {
	if p.polar == nil {
		return
	}

	totalBytes := event.RequestBytes + event.OutputBytes
	typeStr := string(event.Type)

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
		"type": {
			Str: &typeStr,
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

func (p *Client) TrackPromptCallUsage(ctx context.Context, event billing.PromptCallUsageEvent) {
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

func (p *Client) TrackPlatformUsage(ctx context.Context, event billing.PlatformUsageEvent) {
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

func (p *Client) getCustomerState(ctx context.Context, orgID string) (*polarComponents.CustomerState, error) {
	if p == nil || p.polar == nil {
		return nil, fmt.Errorf("polar not initialized")
	}

	customer, err := p.polar.Customers.GetStateExternal(ctx, orgID)
	if err != nil && !strings.Contains(err.Error(), "ResourceNotFound") {
		return nil, fmt.Errorf("query polar customer state: %w", err)
	}

	if customer == nil {
		return nil, nil
	}

	return customer.CustomerState, nil
}

func (p *Client) GetCustomer(ctx context.Context, orgID string) (*billing.Customer, error) {
	var polarCustomerState *polarComponents.CustomerState

	if customerState, err := p.customerStateCache.Get(ctx, polar.OrgCacheKey(orgID)); err == nil {
		polarCustomerState = customerState.CustomerState
	} else {
		polarCustomerState, err = p.getCustomerState(ctx, orgID)
		if err != nil {
			return nil, err
		}

		if err = p.customerStateCache.Store(ctx, polar.PolarCustomerState{OrganizationID: orgID, CustomerState: polarCustomerState}); err != nil {
			p.logger.ErrorContext(ctx, "failed to cache customer state", attr.SlogError(err))
		}
	}

	periodUsage, err := p.extractPeriodUsage(ctx, orgID, polarCustomerState)
	if err != nil {
		return nil, fmt.Errorf("extract period usage: %w", err)
	}

	customerState := &billing.Customer{
		OrganizationID: orgID,
		Tier:           billing.TierFree,
		PeriodUsage:    periodUsage,
	}

	if polarCustomerState != nil {
		for _, sub := range polarCustomerState.ActiveSubscriptions {
			if sub.ProductID == polar.ProductIDPro {
				customerState.Tier = billing.TierBusiness
				break
			}
		}
	}

	return customerState, nil
}

func (p *Client) extractPeriodUsage(ctx context.Context, orgID string, customer *polarComponents.CustomerState) (*gen.PeriodUsage, error) {
	if customer != nil {
		var toolCallMeter *polarComponents.CustomerStateMeter
		var serverMeter *polarComponents.CustomerStateMeter

		for _, meter := range customer.ActiveMeters {
			if meter.MeterID == polar.MeterIDToolCalls {
				toolCallMeter = &meter
			}
			if meter.MeterID == polar.MeterIDServers {
				serverMeter = &meter
			}
		}

		if toolCallMeter == nil || serverMeter == nil {
			return nil, fmt.Errorf(
				"missing meters (tool calls = %s, servers = %s)",
				conv.Ternary(toolCallMeter == nil, "missing", "set"),
				conv.Ternary(serverMeter == nil, "missing", "set"),
			)
		}

		return &gen.PeriodUsage{
			ToolCalls:               int(toolCallMeter.ConsumedUnits),
			MaxToolCalls:            int(toolCallMeter.CreditedUnits),
			Servers:                 int(serverMeter.ConsumedUnits),
			MaxServers:              int(serverMeter.CreditedUnits),
			ActualPublicServerCount: 0, // Not related to polar, popualted elsewhere
		}, nil
	}

	customerFilter := polarOperations.CreateMetersQuantitiesQueryParamExternalCustomerIDFilterStr(orgID)

	// For free tier, we need to read the meter directly because the user won't have a subscription
	toolCallsRes, err := p.polar.Meters.Quantities(ctx, polarOperations.MetersQuantitiesRequest{
		ID:                 polar.MeterIDToolCalls,
		ExternalCustomerID: &customerFilter,
		StartTimestamp:     time.Now().Add(-1 * time.Hour * 24 * 30),
		EndTimestamp:       time.Now(),
		Interval:           polarComponents.TimeIntervalDay,
	})
	if err != nil {
		return nil, fmt.Errorf("get tool call usage: %w", err)
	}

	serversRes, err := p.polar.Meters.Quantities(ctx, polarOperations.MetersQuantitiesRequest{
		ID:                 polar.MeterIDServers,
		ExternalCustomerID: &customerFilter,
		StartTimestamp:     time.Now().Add(-1 * time.Hour * 24 * 30),
		EndTimestamp:       time.Now(),
		Interval:           polarComponents.TimeIntervalDay,
	})
	if err != nil {
		return nil, fmt.Errorf("get server usage: %w", err)
	}

	freeTierProduct, err := p.GetGramFreeTierProduct(ctx)
	if err != nil {
		return nil, fmt.Errorf("get free tier product: %w", err)
	}

	freeTierLimits := polar.ExtractTierLimits(freeTierProduct)
	if freeTierLimits.ToolCalls == 0 || freeTierLimits.Servers == 0 {
		return nil, fmt.Errorf(
			"get free tier limits: missing limits (tool calls = %s, servers = %s)",
			conv.Ternary(freeTierLimits.ToolCalls == 0, "missing", "set"),
			conv.Ternary(freeTierLimits.Servers == 0, "missing", "set"),
		)
	}

	return &gen.PeriodUsage{
		ToolCalls:               int(toolCallsRes.MeterQuantities.Total),
		MaxToolCalls:            freeTierLimits.ToolCalls,
		Servers:                 int(serversRes.MeterQuantities.Total),
		MaxServers:              freeTierLimits.Servers,
		ActualPublicServerCount: 0, // Not related to polar, popualted elsewhere
	}, nil
}

// GetPeriodUsage returns the period usage for the given organization ID as well as their tier limits.
func (p *Client) GetPeriodUsage(ctx context.Context, orgID string) (*gen.PeriodUsage, error) {
	if p.polar == nil {
		return nil, errors.New("polar not initialized")
	}

	customer, err := p.GetCustomer(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("get customer state: %w", err)
	}

	return customer.PeriodUsage, nil
}

func (p *Client) CreateCheckout(ctx context.Context, orgID string, serverURL string) (string, error) {
	if p.polar == nil {
		return "", fmt.Errorf("polar not initialized")
	}

	res, err := p.polar.Checkouts.Create(ctx, polarComponents.CheckoutCreate{
		ExternalCustomerID: &orgID,
		EmbedOrigin:        &serverURL,
		Products: []string{
			polar.ProductIDPro,
		},
	})

	if err != nil {
		return "", fmt.Errorf("create link: %w", err)
	}

	return res.Checkout.URL, nil
}

func (p *Client) CreateCustomerSession(ctx context.Context, orgID string) (string, error) {
	if p.polar == nil {
		return "", fmt.Errorf("polar not initialized")
	}

	res, err := p.polar.CustomerSessions.Create(ctx, polarOperations.CustomerSessionsCreateCustomerSessionCreate{
		CustomerSessionCustomerExternalIDCreate: &polarComponents.CustomerSessionCustomerExternalIDCreate{
			ExternalCustomerID: orgID,
		},
	})

	if err != nil {
		return "", fmt.Errorf("create polar customer session: %w", err)
	}

	return res.CustomerSession.CustomerPortalURL, nil
}

func (p *Client) GetGramFreeTierProduct(ctx context.Context) (*polarComponents.Product, error) {
	if p.polar == nil {
		return nil, fmt.Errorf("polar not initialized")
	}

	res, err := p.polar.Products.Get(ctx, polar.ProductIDFree)
	if err != nil {
		return nil, fmt.Errorf("get polar product: %w", err)
	}

	return res.Product, nil
}

func (p *Client) GetGramProProduct(ctx context.Context) (*polarComponents.Product, error) {
	if p.polar == nil {
		return nil, fmt.Errorf("polar not initialized")
	}

	res, err := p.polar.Products.Get(ctx, polar.ProductIDPro)
	if err != nil {
		return nil, fmt.Errorf("get polar product: %w", err)
	}

	return res.Product, nil
}
