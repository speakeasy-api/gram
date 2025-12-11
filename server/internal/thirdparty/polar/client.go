package polar

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	polargo "github.com/polarsource/polar-go"
	polarComponents "github.com/polarsource/polar-go/models/components"
	polarOperations "github.com/polarsource/polar-go/models/operations"
	"github.com/redis/go-redis/v9"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
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

type MeterQuantities struct {
	Total float32 `json:"total"`
}

type Client struct {
	logger             *slog.Logger
	tracer             trace.Tracer
	polar              *polargo.Polar
	httpClient         *http.Client
	bearerToken        string
	catalog            *Catalog
	customerStateCache cache.TypedCacheObject[PolarCustomerState]
	productCache       cache.TypedCacheObject[Product]
	periodUsageStorage cache.TypedCacheObject[PolarPeriodUsageState]
	webhookSecret      string
}

var _ billing.Tracker = (*Client)(nil)
var _ billing.Repository = (*Client)(nil)

func NewClient(polarClient *polargo.Polar, bearerToken string, logger *slog.Logger, tracerProvider trace.TracerProvider, redisClient *redis.Client, catalog *Catalog, webhookSecret string) *Client {
	return &Client{
		logger:             logger.With(attr.SlogComponent("polar-usage")),
		tracer:             tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/thirdparty/polar"),
		polar:              polarClient,
		httpClient:         &http.Client{Timeout: 30 * time.Second},
		bearerToken:        bearerToken,
		catalog:            catalog,
		customerStateCache: cache.NewTypedObjectCache[PolarCustomerState](logger.With(attr.SlogCacheNamespace("polar-customer-state")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		productCache:       cache.NewTypedObjectCache[Product](logger.With(attr.SlogCacheNamespace("polar-product")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		periodUsageStorage: cache.NewTypedObjectCache[PolarPeriodUsageState](logger.With(attr.SlogCacheNamespace("polar-period-usage-state")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		webhookSecret:      webhookSecret,
	}
}

func (p *Client) getMeterQuantitiesRaw(ctx context.Context, meterID, externalCustomerID string, startTime, endTime time.Time) (*MeterQuantities, error) {
	baseURL := "https://api.polar.sh/v1/meters"
	reqURL := fmt.Sprintf("%s/%s/quantities", baseURL, meterID)

	params := url.Values{}
	params.Add("start_timestamp", startTime.Format(time.RFC3339))
	params.Add("end_timestamp", endTime.Format(time.RFC3339))
	params.Add("interval", "day")
	params.Add("external_customer_id", externalCustomerID)

	fullURL := fmt.Sprintf("%s?%s", reqURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.bearerToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("make request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			p.logger.ErrorContext(ctx, "failed to close response body", attr.SlogError(closeErr))
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var response MeterQuantities
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &response, nil
}

func (p *Client) ValidateAndParseWebhookEvent(ctx context.Context, payload []byte, webhookHeader http.Header) (*billing.PolarWebhookPayload, error) {
	base64Secret := base64.StdEncoding.EncodeToString([]byte(p.webhookSecret))
	wh, err := standardwebhooks.NewWebhook(base64Secret)
	if err != nil {
		return nil, fmt.Errorf("create webhook verifier: %w", err)
	}

	if err := wh.Verify(payload, webhookHeader); err != nil {
		return nil, fmt.Errorf("verify webhook: %w", err)
	}

	var webhookPayload billing.PolarWebhookPayload
	if err := json.Unmarshal(payload, &webhookPayload); err != nil {
		return nil, fmt.Errorf("unmarshal webhook payload: %w", err)
	}

	return &webhookPayload, nil
}

func (p *Client) InvalidateBillingCustomerCaches(ctx context.Context, orgID string) error {
	err := p.customerStateCache.Delete(ctx, PolarCustomerState{OrganizationID: orgID, CustomerState: nil})
	if err != nil {
		return fmt.Errorf("failed to delete customer state cache: %w", err)
	}

	if err := p.periodUsageStorage.Delete(ctx, PolarPeriodUsageState{OrganizationID: orgID, PeriodUsage: gen.PeriodUsage{
		ToolCalls:                0,
		MaxToolCalls:             0,
		Servers:                  0,
		MaxServers:               0,
		ActualEnabledServerCount: 0,
	}}); err != nil {
		return fmt.Errorf("failed todelete period usage storage: %w", err)
	}

	return nil
}

func (p *Client) TrackModelUsage(ctx context.Context, event billing.ModelUsageEvent) {
	ctx, span := p.tracer.Start(ctx, "polar_client.track_model_usage")
	defer span.End()

	source := string(event.Source)
	metadata := map[string]polarComponents.EventMetadataInput{
		"input_tokens": {
			Integer: &event.InputTokens,
		},
		"output_tokens": {
			Integer: &event.OutputTokens,
		},
		"total_tokens": {
			Integer: &event.TotalTokens,
		},
		"model": {
			Str: &event.Model,
		},
		"project_id": {
			Str: &event.ProjectID,
		},
		"source": {
			Str: &source,
		},
		"native_tokens_cached": {
			Integer: &event.NativeTokensCached,
		},
		"native_tokens_reasoning": {
			Integer: &event.NativeTokensReasoning,
		},
		"cache_discount": {
			Number: &event.CacheDiscount,
		},
		"upstream_inference_cost": {
			Number: &event.UpstreamInferenceCost,
		},
	}

	if event.ChatID != "" {
		metadata["chat_id"] = polarComponents.EventMetadataInput{
			Str: &event.ChatID,
		}
	}

	if event.Cost != nil {
		metadata["cost"] = polarComponents.EventMetadataInput{
			Number: event.Cost,
		}
	}

	_, err := p.polar.Events.Ingest(ctx, polarComponents.EventsIngest{
		Events: []polarComponents.Events{
			{
				Type: polarComponents.EventsTypeEventCreateExternalCustomer,
				EventCreateExternalCustomer: &polarComponents.EventCreateExternalCustomer{
					ExternalCustomerID: event.OrganizationID,
					Name:               "model-usage",
					Metadata:           metadata,
				},
			},
		},
	})

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		p.logger.ErrorContext(ctx, "failed to ingest model usage event to Polar", attr.SlogError(err))
	}
}

func (p *Client) TrackToolCallUsage(ctx context.Context, event billing.ToolCallUsageEvent) {
	ctx, span := p.tracer.Start(ctx, "polar_client.track_tool_call_usage")
	defer span.End()

	totalBytes := event.RequestBytes + event.OutputBytes
	typeStr := string(event.Type)

	metadata := map[string]polarComponents.EventMetadataInput{
		"request_bytes": {
			Integer: &event.RequestBytes,
		},
		"output_bytes": {
			Integer: &event.OutputBytes,
		},
		"total_bytes": {
			Integer: &totalBytes,
		},
		"tool_urn": {
			Str: &event.ToolURN,
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

	if event.ResourceURI != "" {
		metadata["resource_uri"] = polarComponents.EventMetadataInput{
			Str: &event.ResourceURI,
		}
	}

	if event.ProjectSlug != nil {
		metadata["project_slug"] = polarComponents.EventMetadataInput{
			Str: event.ProjectSlug,
		}
	}

	if event.OrganizationSlug != nil {
		metadata["organization_slug"] = polarComponents.EventMetadataInput{
			Str: event.OrganizationSlug,
		}
	}

	if event.ToolsetSlug != nil {
		metadata["toolset_slug"] = polarComponents.EventMetadataInput{
			Str: event.ToolsetSlug,
		}
	}

	if event.ChatID != nil {
		metadata["chat_id"] = polarComponents.EventMetadataInput{
			Str: event.ChatID,
		}
	}

	if event.MCPURL != nil {
		metadata["mcp_url"] = polarComponents.EventMetadataInput{
			Str: event.MCPURL,
		}
	}

	if event.FunctionCPUUsage != nil {
		metadata["function_cpu_usage"] = polarComponents.EventMetadataInput{
			Number: event.FunctionCPUUsage,
		}
	}

	if event.FunctionMemUsage != nil {
		metadata["function_mem_usage"] = polarComponents.EventMetadataInput{
			Number: event.FunctionMemUsage,
		}
	}

	if event.FunctionExecutionTime != nil {
		metadata["function_execution_time"] = polarComponents.EventMetadataInput{
			Number: event.FunctionExecutionTime,
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

	metadata := map[string]polarComponents.EventMetadataInput{
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
		metadata["prompt_id"] = polarComponents.EventMetadataInput{
			Str: event.PromptID,
		}
	}

	if event.ProjectSlug != nil {
		metadata["project_slug"] = polarComponents.EventMetadataInput{
			Str: event.ProjectSlug,
		}
	}

	if event.OrganizationSlug != nil {
		metadata["organization_slug"] = polarComponents.EventMetadataInput{
			Str: event.OrganizationSlug,
		}
	}

	if event.ToolsetSlug != nil {
		metadata["toolset_slug"] = polarComponents.EventMetadataInput{
			Str: event.ToolsetSlug,
		}
	}

	if event.ChatID != nil {
		metadata["chat_id"] = polarComponents.EventMetadataInput{
			Str: event.ChatID,
		}
	}

	if event.MCPURL != nil {
		metadata["mcp_url"] = polarComponents.EventMetadataInput{
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

func (p *Client) TrackPlatformUsage(ctx context.Context, events []billing.PlatformUsageEvent) {
	ctx, span := p.tracer.Start(ctx, "polar_client.track_platform_usage")
	defer span.End()

	var polarEvents = make([]polarComponents.Events, 0, len(events))
	for _, event := range events {

		metadata := map[string]polarComponents.EventMetadataInput{
			"public_mcp_servers": {
				Integer: &event.PublicMCPServers,
			},
			"private_mcp_servers": {
				Integer: &event.PrivateMCPServers,
			},
			"total_enabled_servers": {
				Integer: &event.TotalEnabledServers,
			},
			"total_toolsets": {
				Integer: &event.TotalToolsets,
			},
			"total_tools": {
				Integer: &event.TotalTools,
			},
		}

		polarEvents = append(polarEvents, polarComponents.Events{
			Type: polarComponents.EventsTypeEventCreateExternalCustomer,
			EventCreateExternalCustomer: &polarComponents.EventCreateExternalCustomer{
				ExternalCustomerID: event.OrganizationID,
				Name:               "platform-usage",
				Metadata:           metadata,
			},
		})
	}

	_, err := p.polar.Events.Ingest(ctx, polarComponents.EventsIngest{
		Events: polarEvents,
	})

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		p.logger.ErrorContext(ctx, "failed to ingest platform usage event to Polar", attr.SlogError(err))
	}
}

// getCustomerState gets the customer state from the cache or Polar, and stores the result in the cache.
func (p *Client) getCustomerState(ctx context.Context, orgID string) (*polarComponents.CustomerState, error) {
	var polarCustomerState *polarComponents.CustomerState

	if customerState, err := p.customerStateCache.Get(ctx, CustomerStateCacheKey(orgID)); err == nil {
		polarCustomerState = customerState.CustomerState
	} else {
		externalCustomerState, err := p.polar.Customers.GetStateExternal(ctx, orgID)
		if err != nil && !strings.Contains(err.Error(), "ResourceNotFound") {
			return nil, fmt.Errorf("query polar customer state: %w", err)
		}

		if externalCustomerState != nil {
			polarCustomerState = externalCustomerState.CustomerState
		}

		if err = p.customerStateCache.Store(ctx, PolarCustomerState{OrganizationID: orgID, CustomerState: polarCustomerState}); err != nil {
			p.logger.ErrorContext(ctx, "failed to cache customer state", attr.SlogError(err))
		}
	}

	return polarCustomerState, nil
}

// This is used during auth, so keep it as lightweight as possible.
func (p *Client) GetCustomerTier(ctx context.Context, orgID string) (t *billing.Tier, err error) {
	ctx, span := p.tracer.Start(ctx, "polar_client.get_customer_tier", trace.WithAttributes(attr.OrganizationID(orgID)))
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
	if customerState != nil {
		// Active enterprise subscriptions return earlier with the enterprise flag in the DB
		if len(customerState.ActiveSubscriptions) >= 1 {
			return conv.Ptr(billing.TierPro), nil
		}
	}

	return nil, nil
}

func (p *Client) GetCustomer(ctx context.Context, orgID string) (c *billing.Customer, err error) {
	ctx, span := p.tracer.Start(ctx, "polar_client.get_customer", trace.WithAttributes(attr.OrganizationID(orgID)))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	return p.getCustomer(ctx, orgID)
}

func (p *Client) getCustomer(ctx context.Context, orgID string) (*billing.Customer, error) {
	customerState, err := p.getCustomerState(ctx, orgID)
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
		ToolCalls:                -1,
		MaxToolCalls:             -1,
		Servers:                  -1,
		MaxServers:               -1,
		ActualEnabledServerCount: 0, // Not related to polar, popualted elsewhere
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

		// These should almost always be set if we have a Customer, but there are edge cases (such as immediately after a subscription is created) where they are not.
		// In that case, we fall back on reading the meter directly below.
		if toolCallMeter != nil {
			usage.ToolCalls = int(toolCallMeter.ConsumedUnits)

			// Don't set these if they are 0. This can happen for orgs that subscribed but then cancelled.
			// For those, we want to fall back to the free tier limits which will be pulled below if the maxes are still unset.
			if toolCallMeter.CreditedUnits > 0 {
				usage.MaxToolCalls = int(toolCallMeter.CreditedUnits)
			}
		}

		if serverMeter != nil {
			usage.Servers = int(serverMeter.ConsumedUnits)
			if serverMeter.CreditedUnits > 0 {
				usage.MaxServers = int(serverMeter.CreditedUnits)
			}
		}
	}

	/**
	 * If we failed to get the usage from the customer state for any reason, read the usage from the meters directly.
	 * This happens always for free tier, but also in other cases where the customer state is confused
	 */

	if usage.ToolCalls == -1 {
		// For free tier, we need to read the meter directly because the user won't have a subscription
		toolCallsRes, err := p.getMeterQuantitiesRaw(ctx, p.catalog.MeterIDToolCalls, orgID, time.Now().Add(-1*time.Hour*24*30), time.Now())
		if err != nil {
			return nil, fmt.Errorf("get tool call usage: %w", err)
		}

		usage.ToolCalls = int(toolCallsRes.Total)
	}

	if usage.Servers == -1 {
		serversRes, err := p.getMeterQuantitiesRaw(ctx, p.catalog.MeterIDServers, orgID, time.Now().Add(-1*time.Hour*24*30), time.Now())
		if err != nil {
			return nil, fmt.Errorf("get server usage: %w", err)
		}

		usage.Servers = int(serversRes.Total)
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

	if err = p.periodUsageStorage.Store(ctx, PolarPeriodUsageState{OrganizationID: orgID, PeriodUsage: *customer.PeriodUsage}); err != nil {
		p.logger.ErrorContext(ctx, "failed to cache period usage", attr.SlogError(err))
	}

	return customer.PeriodUsage, nil
}

// GetStoredPeriodUsage this enforces that we can only get usage results from a stored value, specifically for hotpath usage with no outbound API call
func (p *Client) GetStoredPeriodUsage(ctx context.Context, orgID string) (pu *gen.PeriodUsage, err error) {
	state, err := p.periodUsageStorage.Get(ctx, PeriodUsageStateCacheKey(orgID))
	if err != nil {
		return nil, fmt.Errorf("get period usage from storage: %w", err)
	}
	return &state.PeriodUsage, nil
}

func (p *Client) CreateCheckout(ctx context.Context, orgID string, serverURL string, successURL string) (u string, err error) {
	ctx, span := p.tracer.Start(ctx, "polar_client.create_checkout", trace.WithAttributes(attr.OrganizationID(orgID)))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	if orgID == "" {
		return "", errors.New("organization ID is required")
	}

	res, err := p.polar.Checkouts.Create(ctx, polarComponents.CheckoutCreate{
		ExternalCustomerID: &orgID,
		EmbedOrigin:        &serverURL,
		SuccessURL:         &successURL,
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
	ctx, span := p.tracer.Start(ctx, "polar_client.create_customer_session", trace.WithAttributes(attr.OrganizationID(orgID)))
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

	// Credits are hard-coded for now; keep bullets in sync dynamically
	freeIncludedCredits := 5
	proIncludedCredits := 25
	additionalToolCallsBlock := 5000

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

	// Helper to format prices cleanly (no trailing .00 for whole dollars)
	formatPrice := func(v float64) string {
		if float64(int(v)) == v {
			return fmt.Sprintf("$%d", int(v))
		}
		return fmt.Sprintf("$%.2f", v)
	}

	return &gen.UsageTiers{
		Free: &gen.TierLimits{
			BasePrice:                  0,
			IncludedToolCalls:          freeTierLimits.ToolCalls,
			IncludedServers:            freeTierLimits.Servers,
			IncludedCredits:            freeIncludedCredits, // Hard coded for now. TODO: Move to Polar
			PricePerAdditionalToolCall: 0,
			PricePerAdditionalServer:   0,
			FeatureBullets: []string{
				"Custom tool creation",
				"Hosted server deployments",
				"14 day log retention",
				"Built in MCP Playground",
				"Connect to Claude, Cursor, Gemini and more",
			},
			IncludedBullets: []string{
				fmt.Sprintf("%d MCP %s (public or private)", freeTierLimits.Servers, conv.Ternary(freeTierLimits.Servers == 1, "server", "servers")),
				fmt.Sprintf("%d tool calls / month", freeTierLimits.ToolCalls),
				fmt.Sprintf("%d chat based credits / month", freeIncludedCredits),
				"Slack community support",
			},
			AddOnBullets: []string{},
		},
		Pro: &gen.TierLimits{
			BasePrice:                  29, // Hard coded for now. TODO: Move to Polar
			IncludedToolCalls:          proTierLimits.ToolCalls,
			IncludedServers:            proTierLimits.Servers,
			IncludedCredits:            proIncludedCredits, // Hard coded for now. TODO: Move to Polar
			PricePerAdditionalToolCall: toolCallPrice,
			PricePerAdditionalServer:   mcpServerPrice,
			FeatureBullets: []string{
				"Custom domain",
				"Register your own OAuth server",
				"30 day log retention",
			},
			IncludedBullets: []string{
				fmt.Sprintf("%d MCP %s (public or private)", proTierLimits.Servers, conv.Ternary(proTierLimits.Servers == 1, "server", "servers")),
				fmt.Sprintf("%d tool calls / month", proTierLimits.ToolCalls),
				fmt.Sprintf("%d chat based credits / month", proIncludedCredits),
				"Email support",
			},
			AddOnBullets: []string{
				fmt.Sprintf("%s / month / additional MCP server", formatPrice(mcpServerPrice)),
				fmt.Sprintf("%s / month / additional %d tool calls", formatPrice(toolCallPrice*float64(additionalToolCallsBlock)), additionalToolCallsBlock),
				"$11 per 10 additional chat based credits", // 1.10 per credit in polar, but this is how we want to label from a marketing perspective
			},
		},
		Enterprise: &gen.TierLimits{
			BasePrice:                  0,
			IncludedToolCalls:          0,
			IncludedServers:            0,
			IncludedCredits:            0,
			PricePerAdditionalToolCall: 0,
			PricePerAdditionalServer:   0,
			FeatureBullets: []string{
				"Oauth 2.1 proxy support",
				"SSO",
				"Audit logs",
				"Self-hosting Gram dataplane",
			},
			IncludedBullets: []string{
				"Dedicated slack channel",
				"Concierge onboarding",
				"Tool design support",
				"SLA-backed support",
			},
			AddOnBullets: []string{},
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
