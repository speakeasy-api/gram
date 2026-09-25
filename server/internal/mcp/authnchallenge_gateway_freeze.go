package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type gatewayConsentTool struct {
	Name               string
	Definition         string
	PreviousDefinition string
	Checked            bool
	Changed            bool
}
type gatewayConsentReview struct {
	Fingerprint string
	Tools       []gatewayConsentTool
	Frozen      bool
	Removed     int
	Error       string
}

func prettyDefinition(raw json.RawMessage) string {
	var buf bytes.Buffer
	if json.Indent(&buf, raw, "", "  ") == nil {
		return buf.String()
	}
	return string(raw)
}
func (s *Service) consentGatewayInventory(ctx context.Context, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) (*toolfilter.FrozenToolset, error) {
	if state.Subject == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	ctx, err := s.contextForSessionSubject(ctx, endpoint, *state.Subject, "", state.ClientID)
	if err != nil {
		return nil, err
	}
	return s.GatewayInventory(ctx, endpoint.MetaMcpServerID.UUID, endpoint.ProjectID, *state.Subject)
}
func (s *Service) gatewayConsentReview(ctx context.Context, endpoint *ResolvedMcpEndpoint, state *AuthnChallengeState, clientID uuid.UUID) (*gatewayConsentReview, error) {
	if state.FirstParty || !endpoint.MetaMcpServerID.Valid || state.Subject == nil {
		return nil, nil
	}
	review := &gatewayConsentReview{Fingerprint: "", Tools: []gatewayConsentTool{}, Frozen: false, Removed: 0, Error: ""}
	previous := map[string]toolfilter.FrozenTool{}
	raw, loadErr := usersessionsrepo.New(s.db).GetLatestLiveUserSessionToolSelection(ctx, usersessionsrepo.GetLatestLiveUserSessionToolSelectionParams{ProjectID: endpoint.ProjectID, OrganizationID: endpoint.OrganizationID, UserSessionIssuerID: endpoint.UserSessionIssuerID, UserSessionClientID: uuid.NullUUID{UUID: clientID, Valid: true}, SubjectUrn: *state.Subject, Resource: endpointToolSelectionResource(endpoint)})
	if loadErr != nil && !errors.Is(loadErr, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load previous gateway approval: %w", loadErr)
	}
	if loadErr == nil {
		policy, err := toolfilter.ParseSessionPolicy(raw)
		if err != nil {
			return nil, fmt.Errorf("read previous gateway approval: %w", err)
		}
		if policy != nil && policy.Gateway != nil && policy.Gateway.Frozen != nil {
			review.Frozen = true
			for _, tool := range policy.Gateway.Frozen.Tools {
				previous[tool.Name] = tool
			}
		}
	}
	if !s.gatewayConnectionOptionsEnabled(ctx, endpoint, productfeatures.FeatureGatewayFrozenToolsets) {
		if !review.Frozen {
			return nil, nil
		}
		review.Error = "Frozen toolset updates are unavailable. Your existing connection stays frozen. Uncheck freezing only if you want a new connection with live tools."
		return review, nil
	}
	snapshot, err := s.consentGatewayInventory(ctx, endpoint, *state)
	if err != nil {
		review.Error = "The full tool list is unavailable. Connect the services above, then reload to review and freeze tools. Any existing frozen connection stays unchanged."
		return review, nil
	}
	fingerprint, err := snapshot.Fingerprint()
	if err != nil {
		review.Error = "This tool list exceeds the size supported by frozen connections."
		return review, nil
	}
	for _, tool := range snapshot.Tools {
		old, existed := previous[tool.Name]
		changed := existed && old.Fingerprint != tool.Fingerprint
		before := ""
		if changed {
			before = prettyDefinition(old.Definition)
		}
		review.Tools = append(review.Tools, gatewayConsentTool{Name: tool.Name, Definition: prettyDefinition(tool.Definition), PreviousDefinition: before, Checked: !review.Frozen || (existed && !changed), Changed: changed})
		delete(previous, tool.Name)
	}
	review.Removed = len(previous)
	review.Fingerprint = fingerprint
	state.GatewayReviewFingerprint = fingerprint
	if err := s.authnChallengeCache.Store(ctx, *state); err != nil {
		return nil, fmt.Errorf("store gateway review binding: %w", err)
	}
	return review, nil
}
func (s *Service) consentFrozenGatewayPolicy(ctx context.Context, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, selectedAgentID string, form url.Values, policy *toolfilter.SessionPolicy) (*toolfilter.SessionPolicy, error) {
	if form.Get("gateway_freeze") != "on" {
		return policy, nil
	}
	if state.FirstParty || selectedAgentID != "" || !s.gatewayConnectionOptionsEnabled(ctx, endpoint, productfeatures.FeatureGatewayFrozenToolsets) {
		return nil, oops.E(oops.CodeBadRequest, nil, "frozen toolsets are not available for this connection")
	}
	if state.GatewayReviewFingerprint == "" || form.Get("gateway_review") != state.GatewayReviewFingerprint {
		return nil, oops.E(oops.CodeConflict, nil, "tool list is unavailable or the review expired; reconnect services and review again, or uncheck freezing to use live tools")
	}
	inventory, err := s.consentGatewayInventory(ctx, endpoint, state)
	if err != nil {
		return nil, oops.E(oops.CodeUnavailable, err, "the complete tool list is unavailable; reconnect and review again")
	}
	frozen, err := inventory.Select(state.GatewayReviewFingerprint, form["gateway_tools"])
	if err != nil {
		return nil, oops.E(oops.CodeConflict, err, "tool definitions changed during review; reload and review again")
	}
	if policy == nil {
		policy = &toolfilter.SessionPolicy{Resource: endpointToolSelectionResource(endpoint), Selection: nil, Gateway: &toolfilter.GatewayOptions{DiscoveryMode: nil, Frozen: nil}}
	}
	policy.Gateway.Frozen = frozen
	if _, err := json.Marshal(policy); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "frozen toolset exceeds supported limits")
	}
	return policy, nil
}
