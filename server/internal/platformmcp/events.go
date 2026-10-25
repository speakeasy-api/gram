package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/mcpevents"
)

const (
	// eventForbidden is the draft's authorization refusal, shared by unknown targets.
	eventForbidden = -32024
	// eventCallbackError follows the ChatGPT profile (-32015), which differs
	// from the working draft (-32027). Neither response exposes callback URLs.
	eventCallbackError = -32015
	// eventNotFound identifies an unsupported event type.
	eventNotFound = -32023
	// eventResourceExhausted reports subscription and request budgets.
	eventResourceExhausted = -32025
)

type eventParams struct {
	mcp.ParamsBase
	mcpevents.SubscriptionInput
}

type eventListParams struct {
	mcp.ParamsBase
	Cursor string `json:"cursor,omitempty"`
}

type eventDefinition struct {
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Delivery      []string        `json:"delivery"`
	InputSchema   json.RawMessage `json:"inputSchema"`
	PayloadSchema json.RawMessage `json:"payloadSchema"`
}

type eventListResult struct {
	mcp.ResultBase
	Events []eventDefinition `json:"events"`
}

type eventSubscriptionResult struct {
	mcp.ResultBase
	mcpevents.SubscriptionResult
}

type eventsCapabilities struct {
	*mcp.ServerCapabilities
	Events *struct{} `json:"events,omitempty"`
}

type eventsDiscovery struct {
	mcp.ResultBase
	mcp.Cacheable
	ResultType        string             `json:"resultType"`
	SupportedVersions []string           `json:"supportedVersions"`
	Capabilities      eventsCapabilities `json:"capabilities"`
	Instructions      string             `json:"instructions,omitempty"`
}

// RegisterReviewEvents extends external discovery and adds the three draft methods.
// It does not add assistant tools or change the ordinary tool catalogue.
func (r *Runtime) RegisterReviewEvents(service *mcpevents.Service, authorizer mcpevents.Authorizer, budget OperationBudget) error {
	if service == nil || authorizer == nil || !budget.valid() {
		return errors.New("review event dependencies are required")
	}
	definitions := []eventDefinition{
		{
			Name:          mcpevents.ReviewRequested,
			Description:   "An MCP server review needs administrator attention in one project. Read the current review queue and policy before preparing a recommendation; an event does not authorize an approval.",
			Delivery:      []string{"webhook"},
			InputSchema:   json.RawMessage(`{"type":"object","properties":{"project_id":{"type":"string","format":"uuid","description":"Explicit project ID returned by list_projects"}},"required":["project_id"],"additionalProperties":false}`),
			PayloadSchema: json.RawMessage(`{"type":"object","properties":{"project_id":{"type":"string"},"request_id":{"type":"string"}},"required":["project_id","request_id"],"additionalProperties":false}`),
		},
		{
			Name:          mcpevents.ReviewDecided,
			Description:   "A decision was recorded for an MCP review you requested in one project. Re-read get_my_mcp_review_request and current install instructions before acting; delayed events do not establish current access.",
			Delivery:      []string{"webhook"},
			InputSchema:   json.RawMessage(`{"type":"object","properties":{"project_id":{"type":"string","format":"uuid","description":"Explicit project ID returned by list_projects"}},"required":["project_id"],"additionalProperties":false}`),
			PayloadSchema: json.RawMessage(`{"type":"object","properties":{"project_id":{"type":"string"},"request_id":{"type":"string"},"decision_id":{"type":"string"}},"required":["project_id","request_id","decision_id"],"additionalProperties":false}`),
		},
	}
	list := func(ctx context.Context) ([]eventDefinition, error) {
		principal, ok := PrincipalFromContext(ctx)
		if !ok {
			return nil, mcpevents.ErrDenied
		}
		owner, err := eventOwner(principal)
		if err != nil {
			return nil, mcpevents.ErrDenied
		}
		visible := make([]eventDefinition, 0, len(definitions))
		for _, definition := range definitions {
			if err := authorizer.Authorize(ctx, owner, uuid.Nil, definition.Name, uuid.Nil); errors.Is(err, mcpevents.ErrDenied) {
				continue
			} else if err != nil {
				return nil, fmt.Errorf("authorize event discovery: %w", err)
			}
			visible = append(visible, definition)
		}
		return visible, nil
	}
	if err := mcp.AddReceivingCustomMethod(r.server, "events/list", func(ctx context.Context, _ *mcp.ServerSession, params *eventListParams) (*eventListResult, error) {
		if params != nil && params.Cursor != "" {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "event catalogue is not paginated", Data: nil}
		}
		visible, err := list(ctx)
		if err != nil {
			return nil, eventRPCError(err)
		}
		return &eventListResult{ResultBase: mcp.ResultBase{Meta: nil}, Events: visible}, nil
	}); err != nil {
		return fmt.Errorf("register event method: %w", err)
	}
	if err := mcp.AddReceivingCustomMethod(r.server, "events/subscribe", func(ctx context.Context, _ *mcp.ServerSession, params *eventParams) (*eventSubscriptionResult, error) {
		if params == nil {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "subscription parameters are required", Data: nil}
		}
		if err := mcpevents.ValidateInput(params.SubscriptionInput, true); err != nil {
			if errors.Is(err, mcpevents.ErrNotFound) {
				return nil, eventRPCError(err)
			}
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: err.Error(), Data: nil}
		}
		principal, ok := PrincipalFromContext(ctx)
		if !ok {
			return nil, eventRPCError(mcpevents.ErrDenied)
		}
		owner, err := eventOwner(principal)
		if err != nil {
			return nil, eventRPCError(mcpevents.ErrDenied)
		}
		if err := budget.Allow(ctx, principal); err != nil {
			return nil, eventRPCError(err)
		}
		result, err := service.Subscribe(ctx, owner, params.SubscriptionInput)
		if err != nil {
			return nil, eventRPCError(err)
		}
		return &eventSubscriptionResult{ResultBase: mcp.ResultBase{Meta: nil}, SubscriptionResult: result}, nil
	}); err != nil {
		return fmt.Errorf("register event method: %w", err)
	}
	if err := mcp.AddReceivingCustomMethod(r.server, "events/unsubscribe", func(ctx context.Context, _ *mcp.ServerSession, params *eventParams) (*mcp.ResultBase, error) {
		if params == nil {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "subscription parameters are required", Data: nil}
		}
		if err := mcpevents.ValidateInput(params.SubscriptionInput, false); err != nil {
			if errors.Is(err, mcpevents.ErrNotFound) {
				return nil, eventRPCError(err)
			}
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: err.Error(), Data: nil}
		}
		principal, ok := PrincipalFromContext(ctx)
		if !ok {
			return nil, eventRPCError(mcpevents.ErrDenied)
		}
		owner, err := eventOwner(principal)
		if err != nil {
			return nil, eventRPCError(mcpevents.ErrDenied)
		}
		if err := service.Unsubscribe(ctx, owner, params.SubscriptionInput); err != nil {
			return nil, eventRPCError(err)
		}
		return &mcp.ResultBase{Meta: nil}, nil
	}); err != nil {
		return fmt.Errorf("register event method: %w", err)
	}
	r.server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, req)
			if err != nil || method != "server/discover" {
				return result, err
			}
			discovery, ok := result.(*mcp.DiscoverResult)
			if !ok {
				return result, nil
			}
			// Capability visibility depends on this authenticated principal.
			discovery.TTLMs = 0
			discovery.CacheScope = "private"
			visible, err := list(ctx)
			if err != nil {
				return nil, eventRPCError(err)
			}
			if len(visible) == 0 {
				return result, nil
			}
			return &eventsDiscovery{
				ResultBase: mcp.ResultBase{Meta: discovery.Meta}, Cacheable: discovery.Cacheable, ResultType: "complete",
				SupportedVersions: discovery.SupportedVersions, Capabilities: eventsCapabilities{ServerCapabilities: discovery.Capabilities, Events: &struct{}{}}, Instructions: discovery.Instructions,
			}, nil
		}
	})
	return nil
}

func eventRPCError(err error) error {
	if errors.Is(err, mcpevents.ErrNotFound) {
		return &jsonrpc.Error{Code: eventNotFound, Message: "Unknown event name", Data: nil}
	}
	if errors.Is(err, mcpevents.ErrLimit) || errors.Is(err, ErrOperationRateLimited) {
		return &jsonrpc.Error{Code: eventResourceExhausted, Message: "Event subscription budget exceeded", Data: nil}
	}
	if errors.Is(err, mcpevents.ErrDenied) {
		return &jsonrpc.Error{Code: eventForbidden, Message: "Event access denied", Data: nil}
	}
	if callback, ok := errors.AsType[*mcpevents.CallbackError](err); ok {
		data, _ := json.Marshal(struct {
			Reason string `json:"reason"`
		}{Reason: callback.Reason})
		return &jsonrpc.Error{Code: eventCallbackError, Message: "Callback endpoint verification failed", Data: data}
	}
	return &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "Events are temporarily unavailable", Data: nil}
}
