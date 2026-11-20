package remotemcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth/protectedresource"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// resourceClient is a remote session client registered for the resource a
// remote MCP server serves.
type resourceClient struct {
	client remotesessionsrepo.ListReachableRemoteSessionClientsRow
}

// resourceDisplay is what a client records about its resource's RFC 9728
// document; the zero value records nothing.
type resourceDisplay struct {
	identifier    string
	name          string
	documentation string
	policyURI     string
	tosURI        string
}

func displayFromDocument(resourceURL string, doc wellknown.OAuthProtectedResourceMetadata) resourceDisplay {
	return resourceDisplay{
		identifier:    resourceURL,
		name:          doc.ResourceName,
		documentation: doc.ResourceDocumentation,
		policyURI:     doc.ResourcePolicyURI,
		tosURI:        doc.ResourceTosURI,
	}
}

// claimOn is the bare claim on resourceURL: what a client records until a
// document for that URL is read.
func claimOn(resourceURL string) resourceDisplay {
	return resourceDisplay{identifier: resourceURL, name: "", documentation: "", policyURI: "", tosURI: ""}
}

// hasMembers reports whether the display carries anything to show; a claim
// without members is a failed probe waiting to be retried.
func (d resourceDisplay) hasMembers() bool {
	return d.name != "" || d.documentation != "" || d.policyURI != "" || d.tosURI != ""
}

func recordedDisplay(c remotesessionsrepo.RemoteSessionClient) resourceDisplay {
	return resourceDisplay{
		identifier:    conv.FromPGTextOrEmpty[string](c.ResourceIdentifier),
		name:          conv.FromPGTextOrEmpty[string](c.ResourceName),
		documentation: conv.FromPGTextOrEmpty[string](c.ResourceDocumentation),
		policyURI:     conv.FromPGTextOrEmpty[string](c.ResourcePolicyUri),
		tosURI:        conv.FromPGTextOrEmpty[string](c.ResourceTosUri),
	}
}

// claimProtectedResource runs inside the transaction that moves the server's
// URL. It finds the clients the Platform MCP attachment registered for this
// server's resource and records the bare claim on the new URL for them, so
// ownership follows the URL transition even when the probe that fills the
// members never completes. A client belongs to the resource when it recorded
// exactly the previous URL as its resource identifier, when it recorded this
// URL with nothing to show, or when it is the registration's only client and
// has never recorded one. Identifiers are matched exactly: /mcp and /mcp/
// may be different resources, and a slash-insensitive match would leave a
// populated client behind on the slashless URL where no later move finds it.
// Returns the clients whose members discovery should fill after commit.
func (s *Service) claimProtectedResource(ctx context.Context, dbtx pgx.Tx, authCtx *contextvalues.AuthContext, serverID uuid.UUID, previousURL, resourceURL string) ([]resourceClient, error) {
	projectID := *authCtx.ProjectID
	offered, err := catalogResourceOffers(ctx, dbtx, authCtx, serverID)
	if err != nil {
		return nil, err
	}
	var clients []resourceClient
	seen := make(map[uuid.UUID]bool)
	q := remotesessionsrepo.New(dbtx)
	for _, registrations := range offered {
		for _, row := range registrations {
			if seen[row.ClientID] {
				continue
			}
			recorded := conv.FromPGTextOrEmpty[string](row.ResourceIdentifier)
			if (recorded == resourceURL && !recordedRowDisplay(row).hasMembers()) ||
				(recorded != resourceURL && (recorded == previousURL || (recorded == "" && len(registrations) == 1))) {
				seen[row.ClientID] = true
				clients = append(clients, resourceClient{client: row})
			}
		}
	}

	// A registration may be offered on multiple targets. Lock the distinct
	// registrations in the same order even when their target sets differ.
	slices.SortFunc(clients, func(a, b resourceClient) int { return bytes.Compare(a.client.ClientID[:], b.client.ClientID[:]) })

	if s.beforeClaim != nil {
		s.beforeClaim(dbtx.Conn().PgConn().PID(), previousURL)
	}
	claim := claimOn(resourceURL)
	claimed := make([]resourceClient, 0, len(clients))
	for _, rc := range clients {
		existing, err := lockResourceClient(ctx, q, authCtx, rc.client.ClientID)
		if err != nil {
			// A client deleted since the list is stale, not a failed save.
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, fmt.Errorf("re-read remote session client: %w", err)
		}
		if existing.UpstreamRejectedAt.Valid || existing.ResourceIdentifier != rc.client.ResourceIdentifier {
			continue
		}
		if recordedDisplay(existing) == claim {
			claimed = append(claimed, rc)
			continue
		}
		if err := s.recordResourceDisplay(ctx, dbtx, authCtx, existing, claim); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, err
		}
		claimed = append(claimed, rc)
	}
	return claimed, nil
}

// catalogResourceOffers resolves registrations for exact live catalogue
// targets. It only considers writable, personal registrations offered there.
func catalogResourceOffers(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext, sourceID uuid.UUID) (map[uuid.UUID][]remotesessionsrepo.ListReachableRemoteSessionClientsRow, error) {
	projectID, org := *authCtx.ProjectID, authCtx.ActiveOrganizationID
	targets, err := platformrepo.New(tx).ListPlatformMCPCatalogOfferTargets(ctx, platformrepo.ListPlatformMCPCatalogOfferTargetsParams{RemoteMcpServerID: conv.ToNullUUID(sourceID), OrganizationID: org, ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("list platform mcp offer targets: %w", err)
	}
	offers, err := upstreamclientsrepo.New(tx).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: org, McpServerIds: targets})
	if err != nil {
		return nil, fmt.Errorf("list catalogue auth offers: %w", err)
	}
	idsByTarget := make(map[uuid.UUID][]uuid.UUID)
	var ids []uuid.UUID
	for _, offer := range offers {
		if offer.ClientType.String != authz.AuthClientTypeRemoteSession || !offer.RemoteSessionClientID.Valid {
			continue
		}
		id := offer.RemoteSessionClientID.UUID
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
		if !slices.Contains(idsByTarget[offer.McpServerID], id) {
			idsByTarget[offer.McpServerID] = append(idsByTarget[offer.McpServerID], id)
		}
	}
	rows, err := remotesessionsrepo.New(tx).ListReachableRemoteSessionClients(ctx, remotesessionsrepo.ListReachableRemoteSessionClientsParams{ProjectID: projectID, OrganizationID: org, RemoteSessionClientIds: ids})
	if err != nil {
		return nil, fmt.Errorf("list reachable catalogue registrations: %w", err)
	}
	byTarget := make(map[uuid.UUID][]remotesessionsrepo.ListReachableRemoteSessionClientsRow)
	for _, row := range rows {
		// Write-time tenant checks reject platform-global or foreign rows.
		if row.UpstreamRejectedAt.Valid || row.CredentialOwner != string(remotesessions.CredentialOwnerSubject) {
			continue
		}
		for _, target := range targets {
			if slices.Contains(idsByTarget[target], row.ClientID) {
				byTarget[target] = append(byTarget[target], row)
			}
		}
	}
	return byTarget, nil
}

// refreshProtectedResourceDisplay re-reads the RFC 9728 document at the
// server's URL and copies its display members onto the clients that claimed
// it. Runs after the URL transition committed so the probe never sits inside
// a transaction. Best effort: the server update stands regardless. A probe
// that fails, or a document that does not name the URL as its resource,
// leaves the bare claim in place — the previous resource's name and links
// must not stand in for this one, and the claim keeps the client selectable
// so the next save retries and a later move still finds it.
func (s *Service) refreshProtectedResourceDisplay(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, serverID uuid.UUID, resourceURL string, clients []resourceClient) {
	// Display members persist as-is, so they are only read over TLS.
	if len(clients) == 0 || !urls.IsAbsoluteHTTPSOrLoopback(resourceURL) {
		return
	}

	projectID := *authCtx.ProjectID
	doc, _, err := wellknown.DiscoverProtectedResourceMetadata(ctx, s.policy, resourceURL)
	if err != nil {
		logger.WarnContext(ctx, "re-probe protected resource metadata", attr.SlogError(err))
		if typed, ok := errors.AsType[*wellknown.ProtectedResourceDiscoveryError](err); ok {
			if err := protectedresource.RecordFetchError(ctx, s.db, projectID, authCtx.ActiveOrganizationID, resourceURL, typed); err != nil {
				logger.ErrorContext(ctx, "record protected resource fetch error", attr.SlogError(err))
			}
		}
		return
	}
	if err := protectedresource.Record(ctx, s.db, projectID, authCtx.ActiveOrganizationID, resourceURL, doc); err != nil {
		logger.ErrorContext(ctx, "record protected resource", attr.SlogError(err))
	}
	if !doc.ValidForResource(resourceURL) {
		logger.WarnContext(ctx, "protected resource metadata resource or location mismatch", attr.SlogURLFull(urls.DiagnosticURL(resourceURL)))
		return
	}
	logScopeComparison(ctx, logger, projectID, serverID, resourceURL, doc, clients)
	display := displayFromDocument(resourceURL, doc)

	for _, rc := range clients {
		if err := s.writeDiscoveredDisplay(ctx, authCtx, serverID, rc, display); err != nil {
			logger.ErrorContext(ctx, "update remote session client resource display", attr.SlogError(err))
		}
	}
}

func recordedRowDisplay(row remotesessionsrepo.ListReachableRemoteSessionClientsRow) resourceDisplay {
	return resourceDisplay{
		identifier:    conv.FromPGTextOrEmpty[string](row.ResourceIdentifier),
		name:          conv.FromPGTextOrEmpty[string](row.ResourceName),
		documentation: conv.FromPGTextOrEmpty[string](row.ResourceDocumentation),
		policyURI:     conv.FromPGTextOrEmpty[string](row.ResourcePolicyUri),
		tosURI:        conv.FromPGTextOrEmpty[string](row.ResourceTosUri),
	}
}

// writeDiscoveredDisplay fills a client's members from the document probed at
// display.identifier, unless the server or the client has since moved on.
func (s *Service) writeDiscoveredDisplay(ctx context.Context, authCtx *contextvalues.AuthContext, serverID uuid.UUID, rc resourceClient, display resourceDisplay) error {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin client display transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := authclientbackfill.LockEMAWriterScope(ctx, dbtx, authCtx.ActiveOrganizationID, []uuid.UUID{*authCtx.ProjectID}); err != nil {
		return fmt.Errorf("lock catalogue resource offer scope: %w", err)
	}

	// Locked so a concurrent save cannot move the URL on between this check and the write.
	server, err := repo.New(dbtx).GetServerByIDForUpdate(ctx, repo.GetServerByIDForUpdateParams{ID: serverID, ProjectID: *authCtx.ProjectID})
	if err != nil {
		return fmt.Errorf("re-read remote mcp server: %w", err)
	}
	if server.Url != display.identifier {
		return nil
	}

	offered, err := catalogResourceOffers(ctx, dbtx, authCtx, serverID)
	if err != nil {
		return err
	}
	stillOffered := false
	for _, registrations := range offered {
		stillOffered = stillOffered || slices.ContainsFunc(registrations, func(row remotesessionsrepo.ListReachableRemoteSessionClientsRow) bool {
			return row.ClientID == rc.client.ClientID
		})
	}
	if !stillOffered {
		return nil
	}

	existing, err := lockResourceClient(ctx, remotesessionsrepo.New(dbtx), authCtx, rc.client.ClientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("re-read remote session client: %w", err)
	}
	recorded := recordedDisplay(existing)
	if existing.UpstreamRejectedAt.Valid || recorded.identifier != display.identifier || recorded == display {
		return nil
	}
	if err := s.recordResourceDisplay(ctx, dbtx, authCtx, existing, display); err != nil {
		return err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit client resource display update: %w", err)
	}
	return nil
}

// lockResourceClient serializes display writes with EMA binding creation.
// The lock only accepts project- and organization-owned writable clients.
func lockResourceClient(ctx context.Context, q *remotesessionsrepo.Queries, authCtx *contextvalues.AuthContext, id uuid.UUID) (remotesessionsrepo.RemoteSessionClient, error) {
	return q.LockEMAClient(ctx, remotesessionsrepo.LockEMAClientParams{ID: id, ProjectID: conv.ToNullUUID(*authCtx.ProjectID), OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID)})
}

// recordResourceDisplay writes display onto the already-locked client and
// audits the change without sourcing authority from compatibility links.
func (s *Service) recordResourceDisplay(ctx context.Context, dbtx pgx.Tx, authCtx *contextvalues.AuthContext, before remotesessionsrepo.RemoteSessionClient, display resourceDisplay) error {
	q := remotesessionsrepo.New(dbtx)
	if conv.FromPGTextOrEmpty[string](before.ResourceIdentifier) != display.identifier {
		count, err := q.CountActiveEMABindingsForClient(ctx, remotesessionsrepo.CountActiveEMABindingsForClientParams{ClientID: conv.ToNullUUID(before.ID), ProjectID: before.ProjectID.UUID, OrganizationID: authCtx.ActiveOrganizationID})
		if err != nil {
			return fmt.Errorf("check resource identity bindings: %w", err)
		}
		if count > 0 {
			return oops.E(oops.CodeConflict, nil, "explicitly unlink identity-chaining bindings before changing the client resource")
		}
	}
	updated, err := q.UpdateRemoteSessionClientResourceDisplay(ctx, remotesessionsrepo.UpdateRemoteSessionClientResourceDisplayParams{
		ResourceIdentifier:    conv.ToPGTextEmpty(display.identifier),
		ResourceName:          display.name,
		ResourceDocumentation: display.documentation,
		ResourcePolicyUri:     display.policyURI,
		ResourceTosUri:        display.tosURI,
		ID:                    before.ID,
		ProjectID:             *authCtx.ProjectID,
		OrganizationID:        authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return fmt.Errorf("update client resource display: %w", err)
	}

	beforeView, err := mv.BuildRemoteSessionClientView(before, nil)
	if err != nil {
		return fmt.Errorf("build client view: %w", err)
	}
	afterView, err := mv.BuildRemoteSessionClientView(updated, nil)
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
	return nil
}
