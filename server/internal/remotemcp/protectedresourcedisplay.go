package remotemcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// resourceClient is a remote session client registered for the resource a
// remote MCP server serves.
type resourceClient struct {
	client remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerRow
}

// refreshProtectedResourceDisplay re-reads the RFC 9728 document at the
// server's new URL and copies its display members onto the clients the
// Platform MCP attachment registered for this server's resource. A client
// belongs to the resource when it recorded the previous URL as its resource
// identifier, or when it is the registration's only client and has never
// recorded one. Best effort: failures are logged and leave the rows as they
// were, and a document that does not name the new URL as its resource writes
// nothing.
func (s *Service) refreshProtectedResourceDisplay(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, serverID uuid.UUID, previousURL, resourceURL string) {
	// Display members persist as-is, so they are only read over TLS.
	if !urls.IsAbsoluteHTTPSOrLoopback(resourceURL) {
		return
	}
	projectID := *authCtx.ProjectID
	registrations, err := platformrepo.New(s.db).ListPlatformMCPCatalogRegistrationsByRemoteMcpServer(ctx, platformrepo.ListPlatformMCPCatalogRegistrationsByRemoteMcpServerParams{
		RemoteMcpServerID: conv.ToNullUUID(serverID),
		OrganizationID:    authCtx.ActiveOrganizationID,
		ProjectID:         projectID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "list platform mcp registrations for remote server", attr.SlogError(err))
		return
	}

	var clients []resourceClient
	q := remotesessionsrepo.New(s.db)
	for _, registration := range registrations {
		if !registration.UserSessionIssuerID.Valid {
			continue
		}
		bound, err := q.ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerParams{
			UserSessionIssuerID: registration.UserSessionIssuerID.UUID,
			ProjectID:           conv.ToNullUUID(projectID),
			OrganizationID:      conv.ToPGText(authCtx.ActiveOrganizationID),
		})
		if err != nil {
			logger.ErrorContext(ctx, "list registration clients", attr.SlogError(err))
			return
		}
		for _, row := range bound {
			recorded := conv.FromPGTextOrEmpty[string](row.ResourceIdentifier)
			if recorded == previousURL || (recorded == "" && len(bound) == 1) {
				clients = append(clients, resourceClient{client: row})
			}
		}
	}
	if len(clients) == 0 {
		return
	}

	doc, _, err := wellknown.DiscoverProtectedResourceMetadata(ctx, s.policy, resourceURL)
	if err != nil {
		logger.WarnContext(ctx, "re-probe protected resource metadata", attr.SlogError(err))
		return
	}
	if !doc.IdentifiesResource(resourceURL) {
		logger.WarnContext(ctx, "protected resource metadata names another resource", attr.SlogURLFull(resourceURL))
		return
	}

	for _, rc := range clients {
		if err := s.writeResourceDisplay(ctx, authCtx, serverID, resourceURL, rc, doc); err != nil {
			logger.ErrorContext(ctx, "update remote session client resource display", attr.SlogError(err))
		}
	}
}

func (s *Service) writeResourceDisplay(ctx context.Context, authCtx *contextvalues.AuthContext, serverID uuid.UUID, resourceURL string, rc resourceClient, doc wellknown.OAuthProtectedResourceMetadata) error {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin client display transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	// Locked so a concurrent save cannot move the URL on between this check and the write.
	server, err := repo.New(dbtx).GetServerByIDForUpdate(ctx, repo.GetServerByIDForUpdateParams{ID: serverID, ProjectID: *authCtx.ProjectID})
	if err != nil {
		return fmt.Errorf("re-read remote mcp server: %w", err)
	}
	if server.Url != resourceURL {
		return nil
	}

	q := remotesessionsrepo.New(dbtx)
	existing, err := q.GetRemoteSessionClientByID(ctx, remotesessionsrepo.GetRemoteSessionClientByIDParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ID:             rc.client.ClientID,
	})
	if err != nil {
		return fmt.Errorf("re-read remote session client: %w", err)
	}
	before := existing.RemoteSessionClient
	if conv.FromPGTextOrEmpty[string](before.ResourceIdentifier) == resourceURL &&
		conv.FromPGTextOrEmpty[string](before.ResourceName) == doc.ResourceName &&
		conv.FromPGTextOrEmpty[string](before.ResourceDocumentation) == doc.ResourceDocumentation &&
		conv.FromPGTextOrEmpty[string](before.ResourcePolicyUri) == doc.ResourcePolicyURI &&
		conv.FromPGTextOrEmpty[string](before.ResourceTosUri) == doc.ResourceTosURI {
		return nil
	}

	updated, err := q.UpdateRemoteSessionClientResourceDisplay(ctx, remotesessionsrepo.UpdateRemoteSessionClientResourceDisplayParams{
		ResourceIdentifier:    conv.ToPGText(resourceURL),
		ResourceName:          doc.ResourceName,
		ResourceDocumentation: doc.ResourceDocumentation,
		ResourcePolicyUri:     doc.ResourcePolicyURI,
		ResourceTosUri:        doc.ResourceTosURI,
		ID:                    rc.client.ClientID,
		ProjectID:             conv.ToNullUUID(*authCtx.ProjectID),
	})
	if err != nil {
		return fmt.Errorf("update client resource display: %w", err)
	}

	beforeView, err := mv.BuildRemoteSessionClientView(before, existing.UserSessionIssuerIds)
	if err != nil {
		return fmt.Errorf("build client view: %w", err)
	}
	afterView, err := mv.BuildRemoteSessionClientView(updated, existing.UserSessionIssuerIds)
	if err != nil {
		return fmt.Errorf("build client view: %w", err)
	}
	if err := s.audit.LogRemoteSessionClientUpdate(ctx, dbtx, audit.LogRemoteSessionClientUpdateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(updated.ID),
		ClientID:               updated.ClientID,
		SnapshotBefore:         beforeView,
		SnapshotAfter:          afterView,
	}); err != nil {
		return fmt.Errorf("audit client resource display update: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit client resource display update: %w", err)
	}
	return nil
}
