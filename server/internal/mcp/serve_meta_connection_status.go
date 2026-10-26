// The gateway's connection_status tool and the MCP Apps view that renders it.

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// handleMetaConnectionStatusCall reports stored consent outcomes for cached
// connections, otherwise the credentials currently available for each member.
// It does not attempt identity chaining or probe upstream servers.
func (s *Service) handleMetaConnectionStatusCall(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
	members []metaMember,
	req *rawRequest,
) (json.RawMessage, error) {
	servers := make([]metamcp.ConnectionStatusServer, 0, len(members)+len(gate.unapprovedMembers)+len(gate.hiddenMembers))
	for _, member := range members {
		status, detail := s.memberSignInStatus(ctx, logger, gate, member)
		servers = append(servers, metamcp.ConnectionStatusServer{
			Slug:   member.slug,
			Name:   member.name,
			Status: status,
			Detail: detail,
		})
	}
	// A cached connection reports members its consent did not approve,
	// whether unavailable at consent or added since, so the person knows
	// renewed consent would add them.
	if gate.catalog != nil && gate.catalogRef != nil && gate.catalogRef.Mode == toolfilter.CatalogModeCached {
		for _, member := range gate.unapprovedMembers {
			servers = append(servers, metamcp.ConnectionStatusServer{
				Slug:   member.slug,
				Name:   member.name,
				Status: metamcp.SignInNeedsConsent,
				Detail: catalogMemberNotApproved,
			})
		}
	}

	for _, member := range gate.hiddenMembers {
		servers = append(servers, metamcp.ConnectionStatusServer{
			Slug:   member.slug,
			Name:   member.name,
			Status: metamcp.SignInHidden,
			Detail: "",
		})
	}

	payload := metamcp.ConnectionStatusResult{ConnectURL: gate.connectURL, Servers: servers}
	structured, err := json.Marshal(payload)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize connection_status result").LogError(ctx, logger)
	}

	chunk, err := json.Marshal(contentChunk[string, json.RawMessage]{
		Type:     "text",
		MimeType: nil,
		Text:     payload.Text(),
		Data:     nil,
		Meta:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize connection_status content").LogError(ctx, logger)
	}
	bs, err := json.Marshal(&result[toolCallResult]{
		ID: req.ID,
		Result: toolCallResult{
			Content:           []json.RawMessage{chunk},
			StructuredContent: structured,
			IsError:           false,
		},
		serverIdentity: serverInfoMetaServer,
		cacheHints:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/call response").LogError(ctx, logger)
	}
	return bs, nil
}

// memberSignInStatus classifies one member by its own routing rule. A member
// that receives no credential and is bound to no sign-in provider (no derived
// remote_session_issuer) needs no sign-in. A remote member can still receive
// a grant routed by its upstream resource without a derived issuer, so
// routing runs before that shortcut.
func (s *Service) memberSignInStatus(ctx context.Context, logger *slog.Logger, gate *metaGateContext, member metaMember) (string, string) {
	if gate.catalog != nil && gate.catalogRef.Mode == toolfilter.CatalogModeCached {
		if err := s.validateCatalogMember(ctx, gate, member); err != nil {
			return metamcp.SignInNeedsConsent, memberErrorMessage(err)
		}
		for _, outcome := range gate.catalog.MemberOutcomes {
			if outcome.Slug == member.slug {
				return metamcp.SignInVerifiedAtConsent, outcome.Label + ". Tool calls check current authorization."
			}
		}
		return metamcp.SignInNeedsConsent, "No recorded consent outcome."
	}
	if member.backend == metaMemberBackendHosted {
		if !member.remoteSessionIssuerID.Valid {
			return metamcp.SignInNotRequired, ""
		}
		gateTokens, err := gate.memberTokens(ctx, member)
		if err != nil {
			return metamcp.SignInError, memberErrorMessage(err)
		}
		tokens, err := hostedMemberTokens(gateTokens, member)
		if err != nil {
			return metamcp.SignInError, memberErrorMessage(err)
		}
		if len(tokens) == 0 {
			return metamcp.SignInNotConnected, ""
		}
		return metamcp.SignInConnected, ""
	}

	probe := *gate
	probe.chainUpstream = nil
	dial, _, err := s.routeMetaMember(ctx, logger, probe, member, "")
	if err != nil {
		return metamcp.SignInError, memberErrorMessage(err)
	}
	if !dial.anonymous {
		return metamcp.SignInConnected, ""
	}
	if !member.remoteSessionIssuerID.Valid {
		return metamcp.SignInNotRequired, ""
	}
	return metamcp.SignInNotConnected, ""
}

func memberErrorMessage(err error) string {
	if memberErr, ok := errors.AsType[*metaMemberError](err); ok {
		return memberErr.message
	}
	return "could not check this server"
}

// handleMetaResourcesList lists the gateway's single UI resource.
func handleMetaResourcesList(ctx context.Context, logger *slog.Logger, req *rawRequest) (json.RawMessage, error) {
	bs, err := json.Marshal(&result[resourcesListResult]{
		ID: req.ID,
		Result: resourcesListResult{Resources: []*resourceListEntry{{
			URI:         metamcp.ConnectionStatusResourceURI,
			Name:        "Gateway sign-in status",
			Description: conv.PtrEmpty("Which member servers you are signed in to."),
			MimeType:    conv.PtrEmpty(metamcp.MCPAppMimeType),
			Meta:        nil,
		}}},
		serverIdentity: serverInfoMetaServer,
		cacheHints:     cacheHintsCallerUniform,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize resources/list response").LogError(ctx, logger)
	}
	return bs, nil
}

// handleMetaResourcesRead serves the static connection_status view. The page
// carries no data; the host delivers the tool result to it.
func handleMetaResourcesRead(ctx context.Context, logger *slog.Logger, req *rawRequest) (json.RawMessage, error) {
	var params resourceReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "failed to parse resources/read request").LogError(ctx, logger)
	}
	if params.URI != metamcp.ConnectionStatusResourceURI {
		return nil, oops.E(oops.CodeNotFound, nil, "unknown resource %q", params.URI).LogWarn(ctx, logger)
	}
	bs, err := json.Marshal(&result[resourceReadResult]{
		ID: req.ID,
		Result: resourceReadResult{Contents: []resourceContent{{
			URI:      metamcp.ConnectionStatusResourceURI,
			Name:     nil,
			MimeType: conv.PtrEmpty(metamcp.MCPAppMimeType),
			Text:     metamcp.ConnectionStatusHTML,
			Blob:     "",
			Meta:     map[string]any{"ui": map[string]any{"prefersBorder": true}},
		}}},
		serverIdentity: serverInfoMetaServer,
		cacheHints:     cacheHintsCallerUniform,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize resources/read response").LogError(ctx, logger)
	}
	return bs, nil
}
