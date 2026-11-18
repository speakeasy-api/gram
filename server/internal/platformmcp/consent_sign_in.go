package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

// ConsentSignInConnector connects the sign-in provider of an MCP server added
// from chat when its card's Connect opens the consent page. The click stands
// in for the confirmation attach_platform_mcp_identity_provider asks for in
// chat, so the user lands on the provider's sign-in, not a dashboard page.
type ConsentSignInConnector struct {
	db            *pgxpool.Pool
	registrations *RegistrationService
	authorizer    Authorizer
}

func NewConsentSignInConnector(db *pgxpool.Pool, registrations *RegistrationService, authorizer Authorizer) *ConsentSignInConnector {
	return &ConsentSignInConnector{db: db, registrations: registrations, authorizer: authorizer}
}

// ConnectPendingSignIn does nothing unless the server is a live Platform MCP
// registration with no sign-in provider yet and the user is a live
// organization admin, the access the chat tool requires.
func (c *ConsentSignInConnector) ConnectPendingSignIn(ctx context.Context, userID, organizationID string, projectID, mcpServerID uuid.UUID) error {
	if c == nil || c.db == nil || c.registrations == nil || c.authorizer == nil {
		return ErrUnavailable
	}
	pending, err := platformrepo.New(c.db).GetPlatformMCPRegistrationAwaitingSignIn(ctx, platformrepo.GetPlatformMCPRegistrationAwaitingSignInParams{
		McpServerID:    uuid.NullUUID{UUID: mcpServerID, Valid: true},
		OrganizationID: organizationID,
		ProjectID:      projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load platform mcp registration awaiting sign-in: %w", err)
	}
	if !isBrowserCatalogProviderKey(pending.CatalogProvider) && pending.CatalogProvider != directRemoteProviderKey {
		return nil
	}
	principal := Principal{
		Surface:        SurfaceDashboard,
		UserID:         userID,
		OrganizationID: organizationID,
		ConnectionID:   "",
		Generation:     "",
		ClientID:       "",
	}
	if err := c.authorizer.RequireLiveOrgAdmin(ctx, principal); err != nil {
		if isAuthorizationDenied(err) {
			return nil
		}
		return fmt.Errorf("authorize platform mcp sign-in connection: %w", err)
	}
	if _, err := c.registrations.AttachDefaultIdentityProvider(ctx, principal, pending.ProjectSlug, pending.ID.String()); err != nil {
		return fmt.Errorf("connect platform mcp sign-in provider: %w", err)
	}
	return nil
}

// signInConnectLinks builds the link a server card's Connect opens: the
// server's own consent page, through the first-party connect entry point.
type signInConnectLinks struct {
	db        *pgxpool.Pool
	serverURL *url.URL
}

// WithSignInConnectLinks lets a card's Connect open the server's consent page.
// Without it Connect opens the server's dashboard setup page.
func (s *RegistrationService) WithSignInConnectLinks(db *pgxpool.Pool, serverURL *url.URL) *RegistrationService {
	if s != nil && db != nil && serverURL != nil {
		s.signInLinks = &signInConnectLinks{db: db, serverURL: serverURL}
	}
	return s
}

// signInURL is the link that connects a server's sign-in, for a result whose
// next action is connect_sign_in: the server's consent page, which connects
// the sign-in provider and starts the provider's sign-in. It is the card's
// Connect button. Empty for any other step, or when the consent page cannot
// be linked; the dashboard setup link is then the way to finish.
func (s *RegistrationService) signInURL(ctx context.Context, principal Principal, project ResolvedProject, registrationID, nextAction string) (string, error) {
	if s == nil || s.signInLinks == nil || nextAction != nextActionConnectSignIn {
		return "", nil
	}
	parsedID, err := uuid.Parse(registrationID)
	if err != nil {
		return "", fmt.Errorf("parse registration id for sign-in link: %w", err)
	}
	slug, err := platformrepo.New(s.signInLinks.db).GetPlatformMCPRegistrationConnectSlug(ctx, platformrepo.GetPlatformMCPRegistrationConnectSlugParams{
		RegistrationID: parsedID,
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// A custom-domain endpoint has no platform consent page to link.
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load platform mcp sign-in link endpoint: %w", err)
	}
	return signInConnectURL(s.signInLinks.serverURL, slug), nil
}

func signInConnectURL(serverURL *url.URL, endpointSlug string) string {
	link := serverURL.JoinPath("mcp", endpointSlug, "connect", "first-party")
	link.RawQuery = url.Values{"setup": {"sign-in"}}.Encode()
	return link.String()
}
