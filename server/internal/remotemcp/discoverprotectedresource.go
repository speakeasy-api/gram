package remotemcp

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// DiscoverProtectedResourceMetadata probes the upstream MCP server identified
// by payload.ID for an RFC 9728 protected-resource metadata document and
// returns either the parsed metadata or a typed unavailability reason.
// Probe failures (including 404 — the expected outcome for non-OAuth resource
// servers) are not errors at this layer; the handler always returns HTTP 200
// with available=false. Only auth, validation, and unexpected database errors
// are returned as errors.
func (s *Service) DiscoverProtectedResourceMetadata(ctx context.Context, payload *gen.DiscoverProtectedResourceMetadataPayload) (*gen.ProtectedResourceMetadataDiscovery, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.RemoteMcpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote mcp server id").Log(ctx, logger)
	}

	server, err := repo.New(s.db).GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server").Log(ctx, logger)
	}

	doc, warnings, probeErr := wellknown.DiscoverProtectedResourceMetadata(ctx, s.policy, server.Url)
	if probeErr != nil {
		if typed, ok := errors.AsType[*wellknown.ProtectedResourceDiscoveryError](probeErr); ok {
			return &gen.ProtectedResourceMetadataDiscovery{
				Available: false,
				Metadata:  nil,
				Unavailable: &gen.ProtectedResourceMetadataUnavailable{
					Code:    typed.Code(),
					Message: typed.UserMessage(),
				},
				DiscoveryWarnings: []string{},
			}, nil
		}
		// The helper always wraps in *ProtectedResourceDiscoveryError; an
		// untyped probe error is a programming bug, not a user-visible
		// upstream failure.
		return nil, oops.E(oops.CodeUnexpected, probeErr, "discover protected resource metadata").Log(ctx, logger)
	}

	return &gen.ProtectedResourceMetadataDiscovery{
		Available: true,
		Metadata: &gen.ProtectedResourceMetadata{
			Resource:               conv.PtrEmpty(doc.Resource),
			AuthorizationServers:   doc.AuthorizationServers,
			ScopesSupported:        doc.ScopesSupported,
			BearerMethodsSupported: doc.BearerMethodsSupported,
			ResourceDocumentation:  conv.PtrEmpty(doc.ResourceDocumentation),
		},
		Unavailable:       nil,
		DiscoveryWarnings: warnings,
	}, nil
}
