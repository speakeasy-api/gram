package platformmcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/trials"
)

// startTrialPath is the dashboard page where an organization admin adds the
// card that starts a self-serve free trial. It sits outside the organization
// routes and acts on the signed-in user's active organization.
const startTrialPath = "start-trial"

// PendingFreeTrial reports that the organization's free trial has not started
// and waits for an organization admin to add a card.
type PendingFreeTrial struct {
	StartURL string `json:"start_url" jsonschema:"dashboard page where an organization admin adds the card that starts the free trial; show it to the user as a clickable link"`
	Message  string `json:"message" jsonschema:"what to tell the user about the free trial"`
}

// FreeTrialStatus answers whether the active organization's self-serve free
// trial waits for a card, with the same predicate the dashboard session uses:
// the rollout flag is on, the organization is not whitelisted, and a validated
// card would start its trial.
type FreeTrialStatus struct {
	logger       *slog.Logger
	db           *pgxpool.Pool
	flags        feature.Provider
	dashboardURL *url.URL
	serverURL    *url.URL
}

// WithFreeTrialStatus lets whoami and get_platform_context link a pending
// organization to the page that starts its free trial. Links need a safe
// dashboard URL; without one the reader reports no pending trial.
func (r *PostgresReader) WithFreeTrialStatus(flags feature.Provider, dashboardURL, serverURL *url.URL) *PostgresReader {
	if r.db == nil || flags == nil || !validDashboardURL(dashboardURL) {
		return r
	}
	dashboardCopy := *dashboardURL
	var serverCopy *url.URL
	if serverURL != nil {
		copied := *serverURL
		serverCopy = &copied
	}
	r.freeTrial = &FreeTrialStatus{logger: r.logger, db: r.db, flags: flags, dashboardURL: &dashboardCopy, serverURL: serverCopy}
	return r
}

// Pending returns the pending free trial of organizationID, or nil when its
// trial does not wait for a card. A lookup failure is logged and reported as
// nil, so the tool still answers.
func (s *FreeTrialStatus) Pending(ctx context.Context, organizationID string) *PendingFreeTrial {
	if s == nil || s.db == nil || organizationID == "" || !feature.SelfServeTrialRequiresPaymentMethod(ctx, s.flags, organizationID) {
		return nil
	}
	organization, err := organizationsrepo.New(s.db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		s.logger.ErrorContext(ctx, "load organization for Platform MCP free trial status", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return nil
	}
	if organization.Whitelisted {
		return nil
	}
	awaiting, err := trials.AwaitingPaymentMethod(ctx, s.db, organization)
	if err != nil {
		s.logger.ErrorContext(ctx, "check Platform MCP free trial status", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return nil
	}
	if !awaiting {
		return nil
	}
	return pendingFreeTrial(requestDashboardURL(ctx, s.dashboardURL, s.serverURL))
}

// pendingFreeTrial builds the pending free trial notice on the dashboard base
// URL. A nil base yields nil: the notice exists to give the user the link.
func pendingFreeTrial(base *url.URL) *PendingFreeTrial {
	if base == nil {
		return nil
	}
	startURL := base.JoinPath(startTrialPath).String()
	return &PendingFreeTrial{
		StartURL: startURL,
		Message:  fmt.Sprintf("This organization's free trial has not started yet. It starts once an organization admin adds a card at %s.", startURL),
	}
}
