package auth

import (
	"context"
	"errors"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// SpeakeasyTeamOrgID is the Gram organization that internal Speakeasy admin
// tooling authenticates as. Admin-only endpoints (e.g. ListAll organizations,
// SetAccountType, SetOrganizationWhitelist) check the caller's
// ActiveOrganizationID against this value. API keys minted inside this org
// pass the gate automatically.
const SpeakeasyTeamOrgID = "5a25158b-24dc-4d49-b03d-e85acfbea59c"

// RequireSpeakeasyTeam returns the caller's auth context iff they belong to
// the speakeasy-team org. Otherwise it returns CodeUnauthorized and logs the
// rejected org ID. `action` is interpolated into the error message (e.g.
// "set organization whitelist status") so handlers keep their specific
// rejection reason without each duplicating the gate.
func RequireSpeakeasyTeam(ctx context.Context, logger *slog.Logger, action string) (*contextvalues.AuthContext, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil {
		return nil, oops.E(oops.CodeUnauthorized, errors.New("missing auth context"), "missing auth context").Log(ctx, logger)
	}
	if ac.ActiveOrganizationID != SpeakeasyTeamOrgID {
		return nil, oops.E(oops.CodeUnauthorized, nil, "only speakeasy-team can %s", action).Log(ctx, logger, attr.SlogOrganizationID(ac.ActiveOrganizationID))
	}
	return ac, nil
}
