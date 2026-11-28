package auth

import (
	"context"

	goa "goa.design/goa/v3/pkg"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const notWhitelistedMessage = "this organization does not have access to Speakeasy yet, please reach out to sales@speakeasy.com"

// whitelistExemptServices stay reachable from a session whose organization is
// not whitelisted. The dashboard sends such a user to billing recovery when
// their trial expires, and paying is how they regain access.
var whitelistExemptServices = map[string]bool{
	"usage": true,
}

// whitelistExemptMethods are single methods outside those services that the
// billing recovery pages need. The dashboard reads the caller's own grants to
// decide whether they may set up billing.
var whitelistExemptMethods = map[string]bool{
	"access.listGrants": true,
}

// requireWhitelistedSession refuses session-authenticated requests for an
// organization that has not been granted access to Speakeasy. The auth service
// authenticates sessions itself, so sign-in, session info, logout and
// organization switching stay reachable. Validated staff support sessions are
// exempt because staff inspect organizations in every state.
func (s *Auth) requireWhitelistedSession(ctx context.Context) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" || authCtx.Whitelisted {
		return nil
	}
	if contextvalues.IsSupportSession(ctx) {
		return nil
	}
	service, _ := ctx.Value(goa.ServiceKey).(string)
	method, _ := ctx.Value(goa.MethodKey).(string)
	if whitelistExemptServices[service] || whitelistExemptMethods[service+"."+method] {
		return nil
	}

	return oops.E(oops.CodeForbidden, nil, notWhitelistedMessage).LogWarn(ctx, s.logger,
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogReason("organization_not_whitelisted"),
		attr.SlogGoaService(service),
		attr.SlogGoaMethod(method),
	)
}
