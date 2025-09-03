package polar

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	polargo "github.com/polarsource/polar-go"
	polarComponents "github.com/polarsource/polar-go/models/components"
	polarOperations "github.com/polarsource/polar-go/models/operations"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/openapi/pointer"
)

type Catalog struct {
	ProductIDFree string
	ProductIDPro  string

	MeterIDToolCalls string
	MeterIDServers   string
}

func (c *Catalog) Validate() error {
	if c.ProductIDFree == "" {
		return errors.New("missing free tier product id in catalog")
	}
	if c.ProductIDPro == "" {
		return errors.New("missing pro tier product id in catalog")
	}
	if c.MeterIDToolCalls == "" {
		return errors.New("missing tool calls meter id in catalog")
	}
	if c.MeterIDServers == "" {
		return errors.New("missing servers meter id in catalog")
	}
	return nil
}

type Client struct {
	logger             *slog.Logger
	tracer             trace.Tracer
	polar              *polargo.Polar
	catalog            *Catalog
	customerStateCache cache.TypedCacheObject[PolarCustomerState]
	productCache       cache.TypedCacheObject[Product]
}

var _ billing.Tracker = (*Client)(nil)
var _ billing.Repository = (*Client)(nil)

func NewClient(polarClient *polargo.Polar, logger *slog.Logger, tracerProvider trace.TracerProvider, redisClient *redis.Client, catalog *Catalog) *Client {
	return &Client{
		logger:             logger.With(attr.SlogComponent("polar-usage")),
		tracer:             tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/thirdparty/polar"),
		polar:              polarClient,
		catalog:            catalog,
		customerStateCache: cache.NewTypedObjectCache[PolarCustomerState](logger.With(attr.SlogCacheNamespace("polar-customer-state")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		productCache:       cache.NewTypedObjectCache[Product](logger.With(attr.SlogCacheNamespace("polar-product")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
	}
}

func (p *Client) TrackToolCallUsage(ctx context.Context, event billing.ToolCallUsageEvent) {
	ctx, span := p.tracer.Start(ctx, "polar_client.track_tool_call_usage")
	defer span.End()

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
		span.SetStatus(codes.Error, err.Error())
		p.logger.ErrorContext(ctx, "failed to ingest usage event to Polar", attr.SlogError(err))
	}
}

func (p *Client) TrackPromptCallUsage(ctx context.Context, event billing.PromptCallUsageEvent) {
	ctx, span := p.tracer.Start(ctx, "polar_client.track_prompt_call_usage")
	defer span.End()

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
		span.SetStatus(codes.Error, err.Error())
		p.logger.ErrorContext(ctx, "failed to ingest usage event to Polar", attr.SlogError(err))
	}
}

func (p *Client) TrackPlatformUsage(ctx context.Context, event billing.PlatformUsageEvent) {
	ctx, span := p.tracer.Start(ctx, "polar_client.track_platform_usage")
	defer span.End()

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
		span.SetStatus(codes.Error, err.Error())
		p.logger.ErrorContext(ctx, "failed to ingest platform usage event to Polar", attr.SlogError(err))
	}
}

// getCustomerState gets the customer state from the cache or Polar, and stores the result in the cache.
func (p *Client) getCustomerState(ctx context.Context, orgID string) (*polarComponents.CustomerState, error) {
	var polarCustomerState *polarComponents.CustomerState

	if customerState, err := p.customerStateCache.Get(ctx, OrgCacheKey(orgID)); err == nil {
		polarCustomerState = customerState.CustomerState
	} else {
		polarCustomerState, err := p.polar.Customers.GetStateExternal(ctx, orgID)
		if err != nil && !strings.Contains(err.Error(), "ResourceNotFound") {
			return nil, fmt.Errorf("query polar customer state: %w", err)
		}

		var state *polarComponents.CustomerState
		if polarCustomerState != nil {
			state = polarCustomerState.CustomerState
		}

		if err = p.customerStateCache.Store(ctx, PolarCustomerState{OrganizationID: orgID, CustomerState: state}); err != nil {
			p.logger.ErrorContext(ctx, "failed to cache customer state", attr.SlogError(err))
		}
	}

	if polarCustomerState == nil {
		return nil, nil
	}

	return polarCustomerState, nil
}

// This is used during auth, so keep it as lightweight as possible.
func (p *Client) GetCustomerTier(ctx context.Context, orgID string) (t *billing.Tier, err error) {
	ctx, span := p.tracer.Start(ctx, "polar_client.get_customer_tier")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	customerState, err := p.getCustomerState(ctx, orgID)
	if err != nil {
		return nil, err
	}

	return p.extractCustomerTier(customerState)
}

func (p *Client) extractCustomerTier(customerState *polarComponents.CustomerState) (*billing.Tier, error) {
	return pointer.From(billing.TierFree), nil // TODO
	if customerState != nil {
		for _, sub := range customerState.ActiveSubscriptions {
			if sub.ProductID == p.catalog.ProductIDPro {
				return conv.Ptr(billing.TierPro), nil
			}
		}
	}

	return nil, nil
}

func (p *Client) GetCustomer(ctx context.Context, orgID string) (c *billing.Customer, err error) {
	ctx, span := p.tracer.Start(ctx, "polar_client.get_customer")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	return p.getCustomer(ctx, orgID)
}

func (p *Client) getCustomer(ctx context.Context, orgID string) (*billing.Customer, error) {
	customerState, err := p.getCustomerState(ctx, "123")//TODO orgID)
	if err != nil {
		return nil, fmt.Errorf("get customer state: %w", err)
	}

	periodUsage, err := p.readPeriodUsage(ctx, orgID, customerState)
	if err != nil {
		return nil, fmt.Errorf("extract period usage: %w", err)
	}

	customer := &billing.Customer{
		OrganizationID: orgID,
		PeriodUsage:    periodUsage,
	}

	return customer, nil
}

// readPeriodUsage reads the period usage from the customer state if available, otherwise reads the usage from the meters directly.
func (p *Client) readPeriodUsage(ctx context.Context, orgID string, customer *polarComponents.CustomerState) (*gen.PeriodUsage, error) {
	usage := gen.PeriodUsage{
		// Set to -1 so we can tell if we've failed to get the usage
		ToolCalls:               -1,
		MaxToolCalls:            -1,
		Servers:                 -1,
		MaxServers:              -1,
		ActualPublicServerCount: 0, // Not related to polar, popualted elsewhere
	}
	
	if customer != nil {
		var toolCallMeter *polarComponents.CustomerStateMeter
		var serverMeter *polarComponents.CustomerStateMeter

		for _, meter := range customer.ActiveMeters {
			if meter.MeterID == p.catalog.MeterIDToolCalls {
				toolCallMeter = &meter
			}
			if meter.MeterID == p.catalog.MeterIDServers {
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

		usage.ToolCalls = int(toolCallMeter.ConsumedUnits)
		usage.MaxToolCalls = int(toolCallMeter.CreditedUnits)
		usage.Servers = int(serverMeter.ConsumedUnits)
		usage.MaxServers = int(serverMeter.CreditedUnits)
	}

	/**
	 * If we failed to get the usage from the customer state for any reason, read the usage from the meters directly.
	 * This happens always for free tier, but also in other cases where the customer state is confused
	 */

	customerFilter := polarOperations.CreateMetersQuantitiesQueryParamExternalCustomerIDFilterStr(orgID)

	if usage.ToolCalls == -1 {
		// For free tier, we need to read the meter directly because the user won't have a subscription
		toolCallsRes, err := p.polar.Meters.Quantities(ctx, polarOperations.MetersQuantitiesRequest{
			ID:                 p.catalog.MeterIDToolCalls,
			ExternalCustomerID: &customerFilter,
			StartTimestamp:     time.Now().Add(-1 * time.Hour * 24 * 30),
			EndTimestamp:       time.Now(),
			Interval:           polarComponents.TimeIntervalDay,
		})
		if err != nil {
			return nil, fmt.Errorf("get tool call usage: %w", err)
		}

		usage.ToolCalls = int(toolCallsRes.MeterQuantities.Total)
	}

	if usage.Servers == -1 {
		serversRes, err := p.polar.Meters.Quantities(ctx, polarOperations.MetersQuantitiesRequest{
			ID:                 p.catalog.MeterIDServers,
			ExternalCustomerID: &customerFilter,
			StartTimestamp:     time.Now().Add(-1 * time.Hour * 24 * 30),
			EndTimestamp:       time.Now(),
			Interval:           polarComponents.TimeIntervalDay,
		})
		if err != nil {
			return nil, fmt.Errorf("get server usage: %w", err)
		}

		usage.Servers = int(serversRes.MeterQuantities.Total)
	}

	if usage.MaxToolCalls == -1 || usage.MaxServers == -1 {
		freeTierProduct, err := p.getProductByID(ctx, p.catalog.ProductIDFree)
		if err != nil {
			return nil, fmt.Errorf("get free tier product: %w", err)
		}

		freeTierLimits := extractTierLimits(p.catalog, freeTierProduct)
		if freeTierLimits.ToolCalls == 0 || freeTierLimits.Servers == 0 {
			return nil, fmt.Errorf(
				"get free tier limits: missing limits (tool calls = %s, servers = %s)",
				conv.Ternary(freeTierLimits.ToolCalls == 0, "missing", "set"),
				conv.Ternary(freeTierLimits.Servers == 0, "missing", "set"),
			)
		}

		usage.MaxToolCalls = freeTierLimits.ToolCalls
		usage.MaxServers = freeTierLimits.Servers
	}

	return &usage, nil
}

// GetPeriodUsage returns the period usage for the given organization ID as well as their tier limits.
func (p *Client) GetPeriodUsage(ctx context.Context, orgID string) (pu *gen.PeriodUsage, err error) {
	ctx, span := p.tracer.Start(ctx, "polar_client.get_period_usage")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	customer, err := p.getCustomer(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("get customer state: %w", err)
	}

	return customer.PeriodUsage, nil
}

func (p *Client) CreateCheckout(ctx context.Context, orgID string, serverURL string) (u string, err error) {
	ctx, span := p.tracer.Start(ctx, "polar_client.create_checkout")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	res, err := p.polar.Checkouts.Create(ctx, polarComponents.CheckoutCreate{
		ExternalCustomerID: &orgID,
		EmbedOrigin:        &serverURL,
		Products: []string{
			p.catalog.ProductIDPro,
		},
	})

	if err != nil {
		return "", fmt.Errorf("create link: %w", err)
	}

	return res.Checkout.URL, nil
}

func (p *Client) CreateCustomerSession(ctx context.Context, orgID string) (cpu string, err error) {
	ctx, span := p.tracer.Start(ctx, "polar_client.create_customer_session")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

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

func (p *Client) GetUsageTiers(ctx context.Context) (ut *gen.UsageTiers, err error) {
	ctx, span := p.tracer.Start(ctx, "polar_client.get_usage_tiers")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	freeTierProduct, err := p.getProductByID(ctx, p.catalog.ProductIDFree)
	if err != nil {
		return nil, fmt.Errorf("failed to load Free tier product: %w", err)
	}

	proTierProduct, err := p.getProductByID(ctx, p.catalog.ProductIDPro)
	if err != nil {
		return nil, fmt.Errorf("failed to load Pro tier product: %w", err)
	}

	freeTierLimits := extractTierLimits(p.catalog, freeTierProduct)
	proTierLimits := extractTierLimits(p.catalog, proTierProduct)

	var toolCallPrice, mcpServerPrice float64

	for _, price := range proTierProduct.Prices {
		if price.Type != polarComponents.PricesTypeProductPrice {
			continue
		}
		if price.ProductPrice == nil || price.ProductPrice.ProductPriceMeteredUnit == nil {
			continue
		}

		if price.ProductPrice.ProductPriceMeteredUnit.MeterID == p.catalog.MeterIDToolCalls {
			meterPrice := *price.ProductPrice.ProductPriceMeteredUnit
			toolCallPrice, err = strconv.ParseFloat(meterPrice.UnitAmount, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse tool call price: %w", err)
			}
			toolCallPrice /= 100 // Result from Polar is in cents
		}

		if price.ProductPrice.ProductPriceMeteredUnit.MeterID == p.catalog.MeterIDServers {
			meterPrice := *price.ProductPrice.ProductPriceMeteredUnit
			mcpServerPrice, err = strconv.ParseFloat(meterPrice.UnitAmount, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse mcp server price: %w", err)
			}
			mcpServerPrice /= 100 // Result from Polar is in cents
		}
	}

	return &gen.UsageTiers{
		Free: &gen.TierLimits{
			BasePrice:                  0,
			IncludedToolCalls:          freeTierLimits.ToolCalls,
			IncludedServers:            freeTierLimits.Servers,
			PricePerAdditionalToolCall: 0,
			PricePerAdditionalServer:   0,
		},
		Business: &gen.TierLimits{
			BasePrice:                  0,
			IncludedToolCalls:          proTierLimits.ToolCalls,
			IncludedServers:            proTierLimits.Servers,
			PricePerAdditionalToolCall: toolCallPrice,
			PricePerAdditionalServer:   mcpServerPrice,
		},
	}, nil
}

func (p *Client) getProductByID(ctx context.Context, id string) (*polarComponents.Product, error) {
	if product, err := p.productCache.Get(ctx, ProductCacheKey(id)); err == nil {
		return &product.Product, nil
	}

	res, err := p.polar.Products.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get polar product: %w", err)
	}

	if err = p.productCache.Store(ctx, Product{Product: *res.Product}); err != nil {
		p.logger.ErrorContext(ctx, "failed to cache product", attr.SlogError(err))
	}

	return res.Product, nil
}
