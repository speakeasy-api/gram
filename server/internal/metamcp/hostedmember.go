// A hosted (toolset-backed) member has no issuer of its own, so the gateway
// registers an OAuth client for its tools' provider on the gateway issuer and
// records that provider on the member row, which consent cards and
// hostedMemberTokens key on. The hosted server's own endpoint is untouched.

package metamcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// HostedMemberProviderAttacher registers and binds an OAuth client for the
// provider protecting an upstream resource. Implemented by
// platformmcp.CatalogIdentityProviderAttachmentService.
type HostedMemberProviderAttacher interface {
	AttachUpstreamProvider(ctx context.Context, principal platformmcp.Principal, project platformmcp.ResolvedProject, input platformmcp.UpstreamProviderAttachmentInput) (platformmcp.UpstreamProviderAttachment, error)
}

// hostedProviderWiring is what wireHostedMemberProvider bound, so the add
// transaction can rebind it if the gateway issuer moved meanwhile and undo it
// if the add does not commit.
type hostedProviderWiring struct {
	clientID       uuid.UUID
	issuerID       uuid.UUID
	remoteIssuerID uuid.UUID
	unbind         func()
}

// wireHostedMemberProvider gives a hosted member the provider identity the
// gateway routes by. No-op for proxied members and hosted members needing no
// upstream OAuth. Runs before the add transaction: discovery and registration
// are network calls.
func (s *Service) wireHostedMemberProvider(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, metaID, mcpServerID uuid.UUID) (hostedProviderWiring, error) {
	none := hostedProviderWiring{clientID: uuid.Nil, issuerID: uuid.Nil, remoteIssuerID: uuid.Nil, unbind: func() {}}
	projectID := *authCtx.ProjectID
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: mcpServerID, ProjectID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return none, nil // the add transaction reports it
	case err != nil:
		return none, oops.E(oops.CodeUnexpected, err, "load mcp server").LogError(ctx, logger)
	}
	if !server.ToolsetID.Valid {
		return none, nil
	}

	provider, err := mcpservers.ResolveHostedOAuthProvider(ctx, s.db, projectID, server.ToolsetID.UUID)
	if cfg, ok := errors.AsType[*mcpservers.HostedProviderError](err); ok {
		return none, oops.E(oops.CodeInvalid, err, "hosted server %q: %s", server.Slug.String, cfg.Reason).LogError(ctx, logger)
	}
	if err != nil {
		return none, oops.E(oops.CodeUnexpected, err, "resolve hosted member provider").LogError(ctx, logger)
	}
	if provider == nil {
		return none, nil
	}
	logger = logger.With(attr.SlogMcpServerID(server.ID.String()), attr.SlogURL(provider.ResourceURL))
	if s.providers == nil {
		return none, oops.E(oops.CodeInvalid, nil, "hosted server %q authenticates users with %s, and this deployment cannot register OAuth clients for gateway members", server.Slug.String, provider.ResourceURL).LogError(ctx, logger)
	}

	meta, err := repo.New(s.db).GetMetaMCPServer(ctx, repo.GetMetaMCPServerParams{ID: metaID, OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return none, oops.E(oops.CodeNotFound, err, "meta mcp server not found").LogError(ctx, logger)
	case err != nil:
		return none, oops.E(oops.CodeUnexpected, err, "load meta mcp server").LogError(ctx, logger)
	case !meta.UserSessionIssuerID.Valid:
		return none, oops.E(oops.CodeInvalid, nil, "meta mcp server has no issuer; update it to mint one before adding a hosted member that needs OAuth").LogError(ctx, logger)
	}

	// A hosted row that already carries an issuer is wired like a proxied
	// member: client on its own issuer, derived issuer resynced, and the add
	// transaction's auto-attach copies it to the gateway.
	bindIssuerID := meta.UserSessionIssuerID.UUID
	if server.UserSessionIssuerID.Valid {
		bindIssuerID = server.UserSessionIssuerID.UUID
	}
	attached, err := s.providers.AttachUpstreamProvider(ctx,
		platformmcp.Principal{UserID: authCtx.UserID, OrganizationID: authCtx.ActiveOrganizationID, ConnectionID: "", Generation: "", ClientID: "", Surface: platformmcp.SurfaceDashboard},
		platformmcp.ResolvedProject{ID: projectID, Name: "", Slug: ""},
		platformmcp.UpstreamProviderAttachmentInput{UserSessionIssuerID: bindIssuerID, ResourceURL: provider.ResourceURL, IssuerSlug: hostedProviderIssuerSlug, IssuerName: provider.Name})
	if err != nil {
		return none, hostedMemberProviderError(ctx, logger, server.Slug.String, provider.ResourceURL, err)
	}
	wiring := hostedProviderWiring{clientID: attached.ClientID, issuerID: bindIssuerID, remoteIssuerID: attached.RemoteSessionIssuerID, unbind: func() {}}
	if attached.Bound {
		wiring.unbind = func() {
			// Another hosted member committed on this provider meanwhile: the
			// binding is theirs now.
			if n, cerr := repo.New(s.db).CountHostedMetaMCPMembersOnProvider(ctx, repo.CountHostedMetaMCPMembersOnProviderParams{
				MetaMcpServerID: metaID,
				ProjectID:       projectID,
				RemoteIssuerID:  uuid.NullUUID{UUID: attached.RemoteSessionIssuerID, Valid: true},
			}); cerr != nil || n > 0 {
				return
			}
			if _, derr := remotesessionsrepo.New(s.db).DetachRemoteSessionClientFromUserSessionIssuer(ctx, remotesessionsrepo.DetachRemoteSessionClientFromUserSessionIssuerParams{
				RemoteSessionClientID: attached.ClientID,
				UserSessionIssuerID:   bindIssuerID,
			}); derr != nil {
				logger.ErrorContext(ctx, "unbind hosted member provider client after failed add", attr.SlogError(derr))
			}
		}
	}

	if reason, err := s.hostedGrantQualifiedBy(ctx, authCtx, metaID, bindIssuerID, attached.RemoteSessionIssuerID); err != nil {
		wiring.unbind()
		return none, oops.E(oops.CodeUnexpected, err, "check hosted member provider routing").LogError(ctx, logger)
	} else if reason != "" {
		wiring.unbind()
		return none, oops.E(oops.CodeConflict, nil, "%s already authenticates with %s; a gateway routes one credential per provider", reason, attached.IssuerURL).LogError(ctx, logger)
	}

	if server.UserSessionIssuerID.Valid {
		err = remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, s.db, authCtx.ActiveOrganizationID, projectID, []uuid.UUID{bindIssuerID})
	} else {
		var stamped int64
		stamped, err = mcpserversrepo.New(s.db).StampHostedMCPServerProviderIssuer(ctx, mcpserversrepo.StampHostedMCPServerProviderIssuerParams{
			RemoteSessionIssuerID: uuid.NullUUID{UUID: attached.RemoteSessionIssuerID, Valid: true},
			ID:                    server.ID,
			ProjectID:             projectID,
			ToolsetID:             server.ToolsetID,
		})
		if err == nil && stamped != 1 {
			err = fmt.Errorf("stamped %d rows", stamped)
		}
	}
	if err != nil {
		wiring.unbind()
		return none, oops.E(oops.CodeUnexpected, err, "record hosted member provider").LogError(ctx, logger)
	}

	logger.InfoContext(ctx, "wired hosted member upstream provider",
		attr.SlogMetaMcpServerID(metaID.String()),
		attr.SlogRemoteSessionIssuerID(attached.RemoteSessionIssuerID.String()))
	return wiring, nil
}

// rebindHostedProvider moves the provider binding onto the gateway issuer the
// add transaction locked when an issuer change landed between wiring and the
// lock, so the new member is never left bound to an issuer the gateway no
// longer uses.
func rebindHostedProvider(ctx context.Context, dbtx pgx.Tx, wiring hostedProviderWiring, gatewayIssuerID uuid.UUID) error {
	if wiring.clientID == uuid.Nil || wiring.issuerID == gatewayIssuerID {
		return nil
	}
	q := remotesessionsrepo.New(dbtx)
	if err := q.LockRemoteSessionIssuerForClientBinding(ctx, wiring.remoteIssuerID); err != nil {
		return fmt.Errorf("lock remote session issuer for client binding: %w", err)
	}
	if err := q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: wiring.clientID, UserSessionIssuerID: gatewayIssuerID}); err != nil {
		return fmt.Errorf("bind provider client to gateway issuer: %w", err)
	}
	if _, err := q.DetachRemoteSessionClientFromUserSessionIssuer(ctx, remotesessionsrepo.DetachRemoteSessionClientFromUserSessionIssuerParams{RemoteSessionClientID: wiring.clientID, UserSessionIssuerID: wiring.issuerID}); err != nil {
		return fmt.Errorf("unbind provider client from previous gateway issuer: %w", err)
	}
	return nil
}

// hostedGrantQualifiedBy mirrors consent-time resource derivation and names
// what would qualify the grant to a URL: a proxied member claiming the provider
// (resolveMetaMemberResource) or a remote server sharing the client
// (FallbackResourceForClient). hostedMemberTokens accepts unqualified grants
// only. Empty when nothing does.
func (s *Service) hostedGrantQualifiedBy(ctx context.Context, authCtx *contextvalues.AuthContext, metaID, issuerID, remoteIssuerID uuid.UUID) (string, error) {
	projectID := *authCtx.ProjectID
	claimants, err := repo.New(s.db).ListMetaMCPMembersForRemoteSessionIssuer(ctx, repo.ListMetaMCPMembersForRemoteSessionIssuerParams{
		MetaMcpServerID:       metaID,
		ProjectID:             projectID,
		RemoteSessionIssuerID: uuid.NullUUID{UUID: remoteIssuerID, Valid: true},
	})
	if err != nil {
		return "", fmt.Errorf("list meta mcp members for provider: %w", err)
	}
	if len(claimants) > 0 {
		return "another member of this meta mcp server", nil
	}

	rsRepo := remotesessionsrepo.New(s.db)
	clients, err := rsRepo.ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: issuerID,
		ProjectID:           conv.ToNullUUID(projectID),
		OrganizationID:      conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		return "", fmt.Errorf("list provider clients: %w", err)
	}
	for _, client := range clients {
		if client.RemoteSessionIssuerID != remoteIssuerID {
			continue
		}
		servers, err := rsRepo.ListOrganizationMcpServersForClient(ctx, client.ClientID)
		if err != nil {
			return "", fmt.Errorf("list servers for provider client: %w", err)
		}
		for _, server := range servers {
			if server.Url != "" {
				return "a remote server sharing the same OAuth client", nil
			}
		}
	}
	return "", nil
}

func hostedMemberProviderError(ctx context.Context, logger *slog.Logger, serverSlug, resourceURL string, err error) error {
	switch {
	case errors.Is(err, platformmcp.ErrIdentityProviderAttachmentUnsupported):
		return oops.E(oops.CodeInvalid, err, "hosted server %q authenticates users with %s, which offers no OAuth client registration Gram can use, so it cannot join a gateway", serverSlug, resourceURL).LogError(ctx, logger)
	case errors.Is(err, platformmcp.ErrIdentityProviderAttachmentConflict):
		return oops.E(oops.CodeConflict, err, "the OAuth provider for hosted server %q (%s) conflicts with one already registered in this project", serverSlug, resourceURL).LogError(ctx, logger)
	default:
		return oops.E(oops.CodeUnavailable, err, "could not register an OAuth client with %s for hosted server %q; retry shortly", resourceURL, serverSlug).LogError(ctx, logger)
	}
}

func hostedProviderIssuerSlug(issuerURL string) string {
	sum := sha256.Sum256([]byte(strings.TrimRight(issuerURL, "/")))
	return "gateway-provider-" + hex.EncodeToString(sum[:8])
}
