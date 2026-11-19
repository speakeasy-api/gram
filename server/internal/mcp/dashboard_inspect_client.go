package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const (
	dashboardInspectClientID     = "speakeasy-dashboard-inspect"
	dashboardInspectCallbackPath = "/inspect/oauth/callback"
)

// dashboardInspectRedirectURIs is deployment configuration, not request data.
// The dashboard can run on its own origin (including each dev worktree), or
// on the server and configured first-party aliases in production.
func (s *Service) dashboardInspectRedirectURIs() []string {
	var callbacks []string
	add := func(base *url.URL) {
		if base == nil || base.Hostname() == "" || base.User != nil {
			return
		}
		if base.Scheme != "https" && (base.Scheme != "http" || !isLoopbackDashboardHost(base.Hostname())) {
			return
		}
		callback := (&url.URL{Scheme: base.Scheme, Host: base.Host, Path: dashboardInspectCallbackPath}).String()
		if !slices.Contains(callbacks, callback) {
			callbacks = append(callbacks, callback)
		}
	}
	add(s.siteURL)
	add(s.serverURL)
	for _, origin := range s.platformHosts {
		if base, err := url.Parse(origin); err == nil {
			add(base)
		}
	}
	slices.Sort(callbacks)
	return callbacks
}

func isLoopbackDashboardHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
}

// registerDashboardInspectClient lazily provisions the public client at the
// issuer selected by /authorize. The usual consent, grant and runtime paths
// resolve its persisted client row like any other OAuth client.
func (s *Service) registerDashboardInspectClient(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, redirectURIs []string) (*usersessions_repo.UserSessionClient, error) {
	client, err := s.resolveUserSessionClient(ctx, logger, endpoint, dashboardInspectClientID, lookupClientOnly)
	if err == nil {
		// Only current deployment configuration is authoritative for redirects,
		// even when the row predates an origin change.
		client.RedirectUris = redirectURIs
		return client, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	var noText pgtype.Text
	var noExpiry pgtype.Timestamptz
	row, err := usersessions_repo.New(s.db).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     endpoint.UserSessionIssuerID,
		ClientID:                dashboardInspectClientID,
		ClientName:              "Speakeasy Dashboard Inspect",
		RedirectUris:            redirectURIs,
		TokenEndpointAuthMethod: oauthwire.AuthMethodNone,
		ClientSecretHash:        noText,
		ClientSecretExpiresAt:   noExpiry,
		ClientJwks:              nil,
		ClientJwksUri:           noText,
	})
	if isUniqueViolation(err) {
		// Another request already registered the client for this issuer.
		client, err = s.resolveUserSessionClient(ctx, logger, endpoint, dashboardInspectClientID, lookupClientOnly)
		if err == nil {
			client.RedirectUris = redirectURIs
		}
		return client, err
	}
	if err != nil {
		return nil, fmt.Errorf("register dashboard Inspect OAuth client: %w", err)
	}
	return &row, nil
}
