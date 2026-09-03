// Consent-time tool inventory snapshots. The consent island performs a real
// MCP session (initialize → paginated tools/list) against the consent-scoped
// transport; every tools/list page the server RELAYS is captured into a
// per-attempt snapshot, and approval binds to a snapshot only once it is
// complete — so the exact inventory shown to the user is the one the
// selection is validated against.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	// consentToolInventoryTTL matches the authn-challenge state TTL: a
	// snapshot is only addressable through a live state, and approval
	// consumes the state, so a longer TTL would only keep dead entries.
	consentToolInventoryTTL = 10 * time.Minute

	// consentInventoryMaxTools bounds upstream-controlled inventory size; a
	// server exceeding it fails the attempt rather than truncating, which
	// would present an incomplete approval surface.
	consentInventoryMaxTools = 1000

	// consentInventoryMaxNameBytes bounds a single upstream tool name.
	consentInventoryMaxNameBytes = 200

	// consentInventoryMaxCursorBytes bounds the upstream-controlled
	// pagination cursor stored on the draft.
	consentInventoryMaxCursorBytes = 2048

	// consentInventoryMaxSessionIDBytes bounds the upstream-assigned MCP
	// session id recorded for DELETE binding.
	consentInventoryMaxSessionIDBytes = 512
)

// errConsentInventoryUnavailable marks a snapshot-store (Redis) dependency
// failure: approval cannot bind without it.
var errConsentInventoryUnavailable = errors.New("consent tool inventory store unavailable")

// consentInventoryTool is one captured tool: name plus the known annotation
// values whose raw hints are explicitly true.
type consentInventoryTool struct {
	Name        string   `json:"name"`
	Annotations []string `json:"annotations"`
}

// consentToolInventory is the per-(state, attempt) snapshot. It accumulates
// across tools/list pages and becomes immutable once Complete.
type consentToolInventory struct {
	// StateID keys the snapshot to one authn challenge.
	StateID string `json:"state_id"`

	// Attempt is the island's per-hydration UUID; a retry starts a fresh
	// attempt so a half-fetched inventory can never satisfy approval.
	Attempt string `json:"attempt"`

	// Resource is the endpointToolSelectionResource the selection will bind
	// to.
	Resource string `json:"resource"`

	// Tools accumulates captured pages in relay order.
	Tools []consentInventoryTool `json:"tools"`

	// ExpectedCursor is the cursor the next captured tools/list request must
	// carry; empty means the first page. Enforced so pages append in
	// protocol order.
	ExpectedCursor string `json:"expected_cursor"`

	// Complete flips when the page with an empty nextCursor is captured —
	// before that page is relayed — and is required at approval.
	Complete bool `json:"complete"`

	// McpSessionID records the upstream session the attempt's handshake
	// established; the consent transport's DELETE only terminates an
	// exactly matching session.
	McpSessionID string `json:"mcp_session_id"`
}

func consentToolInventoryCacheKey(stateID, attempt string) string {
	return "consentToolInventory:" + stateID + ":" + attempt
}

// CacheKey implements cache.CacheableObject.
func (i consentToolInventory) CacheKey() string {
	return consentToolInventoryCacheKey(i.StateID, i.Attempt)
}

// AdditionalCacheKeys implements cache.CacheableObject.
func (i consentToolInventory) AdditionalCacheKeys() []string { return []string{} }

// TTL implements cache.CacheableObject.
func (i consentToolInventory) TTL() time.Duration { return consentToolInventoryTTL }

// consentAttemptID validates the island-supplied attempt id. UUID-shaped so
// the Redis key space stays bounded and log-safe.
func consentAttemptID(raw string) (string, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse consent inventory attempt id: %w", err)
	}
	return id.String(), nil
}

// loadConsentInventoryDraft reads the attempt's draft, creating the initial
// empty draft on first use.
func (s *Service) loadConsentInventoryDraft(ctx context.Context, endpoint *ResolvedMcpEndpoint, stateID, attempt string) (consentToolInventory, error) {
	cached, err := s.consentToolInventoryCache.Get(ctx, consentToolInventoryCacheKey(stateID, attempt))
	if err == nil {
		return cached, nil
	}
	if !errors.Is(err, redisCache.ErrCacheMiss) {
		return consentToolInventory{}, fmt.Errorf("%w: %w", errConsentInventoryUnavailable, err)
	}
	return consentToolInventory{
		StateID:        stateID,
		Attempt:        attempt,
		Resource:       endpointToolSelectionResource(endpoint),
		Tools:          []consentInventoryTool{},
		ExpectedCursor: "",
		Complete:       false,
		McpSessionID:   "",
	}, nil
}

// appendConsentInventoryPage validates one captured tools/list page against
// the draft's cursor chain and caps, appends it, and marks the snapshot
// complete when nextCursor is empty. The store happens BEFORE the page is
// relayed to the island, so a snapshot the island saw is always at least as
// complete as what approval reads. Returns the updated draft.
func (s *Service) appendConsentInventoryPage(
	ctx context.Context,
	draft consentToolInventory,
	requestCursor string,
	pageTools []consentInventoryTool,
	nextCursor string,
) (consentToolInventory, error) {
	if draft.Complete {
		return draft, fmt.Errorf("consent inventory attempt is already complete")
	}
	if requestCursor != draft.ExpectedCursor {
		return draft, fmt.Errorf("tools/list cursor is out of order for this attempt")
	}

	seen := make(map[string]bool, len(draft.Tools)+len(pageTools))
	for _, tool := range draft.Tools {
		seen[tool.Name] = true
	}
	for idx := range pageTools {
		tool := &pageTools[idx]
		if tool.Name == "" {
			return draft, fmt.Errorf("inventory contains a blank tool name")
		}
		if len(tool.Name) > consentInventoryMaxNameBytes {
			return draft, fmt.Errorf("inventory tool name exceeds %d bytes", consentInventoryMaxNameBytes)
		}
		if seen[tool.Name] {
			return draft, fmt.Errorf("inventory contains duplicate tool name %q", tool.Name)
		}
		seen[tool.Name] = true
		tool.Annotations = normalizeKnownAnnotations(tool.Annotations)
	}
	if len(draft.Tools)+len(pageTools) > consentInventoryMaxTools {
		return draft, fmt.Errorf("inventory exceeds %d tools", consentInventoryMaxTools)
	}

	if len(nextCursor) > consentInventoryMaxCursorBytes {
		return draft, fmt.Errorf("inventory pagination cursor exceeds %d bytes", consentInventoryMaxCursorBytes)
	}
	draft.Tools = append(draft.Tools, pageTools...)
	draft.ExpectedCursor = nextCursor
	if nextCursor == "" {
		slices.SortFunc(draft.Tools, func(a, b consentInventoryTool) int {
			return strings.Compare(a.Name, b.Name)
		})
		draft.Complete = true
	}
	if err := s.consentToolInventoryCache.Store(ctx, draft); err != nil {
		return draft, fmt.Errorf("%w: store inventory snapshot: %w", errConsentInventoryUnavailable, err)
	}
	return draft, nil
}

// getCompletedConsentInventory reads the approval-time snapshot.
// found=false with nil error covers a missing OR incomplete snapshot —
// both retryable by reloading — while an error means the store itself is
// unavailable.
func (s *Service) getCompletedConsentInventory(ctx context.Context, stateID, attempt string) (consentToolInventory, bool, error) {
	var zero consentToolInventory
	cached, err := s.consentToolInventoryCache.Get(ctx, consentToolInventoryCacheKey(stateID, attempt))
	if err != nil {
		if errors.Is(err, redisCache.ErrCacheMiss) {
			return zero, false, nil
		}
		return zero, false, fmt.Errorf("%w: %w", errConsentInventoryUnavailable, err)
	}
	if !cached.Complete {
		return zero, false, nil
	}
	return cached, true, nil
}

// evictConsentToolInventory drops every attempt snapshot for a state after a
// terminal approve/deny. Best effort: the challenge state is already
// consumed, so a leftover snapshot cannot authorize anything.
func (s *Service) evictConsentToolInventory(ctx context.Context, stateID string) {
	if err := s.consentToolInventoryCache.DeleteByPrefix(ctx, "consentToolInventory:"+stateID+":"); err != nil {
		s.logger.WarnContext(ctx, "evict consent tool inventory", attr.SlogError(err))
	}
}

// normalizeKnownAnnotations filters to the known vocabulary and emits it in
// toolfilter.KnownAnnotations order, never nil.
func normalizeKnownAnnotations(values []string) []string {
	out := []string{}
	for _, known := range toolfilter.KnownAnnotations {
		if slices.Contains(values, known) {
			out = append(out, known)
		}
	}
	return out
}

// enumerateToolsetConsentInventory materializes the toolset-backed inventory
// through describeConsentToolset — the same variation-group and model-view
// resolution the runtime's tools/list uses. Nested external-MCP placeholders
// are excluded exactly as restrictive runtime filtering excludes them.
// enumerateToolsetConsentInventory resolves the endpoint's inventory the
// same way the runtime's tools/list will serve it, including the per-tool
// RBAC gate for private servers — the picker must show exactly what the
// minted session can reach, and a grant must never name a tool the
// caller's role hides. roleHidden carries the RBAC-dropped tool names so
// the island can show the subject what their role excluded; the subject is
// an authenticated member of the org that owns the server, and only names
// are revealed — never schemas, descriptions, or the ability to grant.
func (s *Service) enumerateToolsetConsentInventory(ctx context.Context, endpoint *ResolvedMcpEndpoint) (tools []consentInventoryTool, roleHidden []string, err error) {
	toolset, err := s.describeConsentToolset(ctx, endpoint)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve toolset for consent inventory: %w", err)
	}
	if toolset == nil {
		return nil, nil, fmt.Errorf("endpoint is not toolset-backed")
	}

	// Wrapper visibility wins; only the legacy endpoint reads the toolset flag.
	private := !endpoint.IsPublic
	if !endpoint.McpServerID.Valid {
		private = toolset.McpIsPublic == nil || !*toolset.McpIsPublic
	}
	connectResourceID := endpoint.connectResourceID().String()
	tools = []consentInventoryTool{}
	for _, tool := range toolset.Tools {
		if tool == nil || conv.IsProxyTool(tool) {
			continue
		}
		base, berr := conv.ToBaseTool(tool)
		if berr != nil {
			continue
		}
		values := trueAnnotationValues(base.Annotations)
		if s.authz != nil && private {
			// Vocabulary order matches the RBAC disposition collapse, so the
			// first true hint IS the disposition dimension tools/list uses.
			disposition := ""
			if len(values) > 0 {
				disposition = values[0]
			}
			if rerr := s.authz.Require(ctx, authz.MCPToolCallCheck(connectResourceID, authz.MCPToolCallDimensions{
				Tool:        base.Name,
				Disposition: disposition,
				ProjectID:   endpoint.ProjectID.String(),
			})); rerr != nil {
				var oopsErr *oops.ShareableError
				if errors.As(rerr, &oopsErr) && oopsErr.Code == oops.CodeForbidden {
					roleHidden = append(roleHidden, base.Name)
					continue
				}
				return nil, nil, fmt.Errorf("check tool-level authz for consent inventory: %w", rerr)
			}
		}
		tools = append(tools, consentInventoryTool{
			Name:        base.Name,
			Annotations: values,
		})
	}
	return tools, roleHidden, nil
}
