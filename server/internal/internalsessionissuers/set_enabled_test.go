package internalsessionissuers_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/internal_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestSetEnabled_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	for _, enabled := range []bool{true, false} {
		_, err := ti.service.SetEnabled(readCtx(t, ctx), &gen.SetEnabledPayload{Enabled: enabled, SessionToken: nil})
		requireOopsCode(t, err, oops.CodeForbidden)
	}
}

func TestMutations_RefuseSupportSessions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	admin := adminCtx(t, ctx)
	kid := enable(t, ti, admin)
	authCtx, ok := contextvalues.GetAuthContext(admin)
	require.True(t, ok)
	support := *authCtx
	support.IsAdmin = true
	support.SupportOrganizationID = ti.orgID
	asSupport := contextvalues.WithValidatedSupportSession(admin, &support)

	for _, enabled := range []bool{true, false} {
		_, err := ti.service.SetEnabled(asSupport, &gen.SetEnabledPayload{Enabled: enabled, SessionToken: nil})
		requireOopsCode(t, err, oops.CodeForbidden)
	}
	for _, compromised := range []bool{true, false} {
		_, err := ti.service.Rotate(asSupport, &gen.RotatePayload{ActiveKid: &kid, Compromised: compromised, SessionToken: nil})
		requireOopsCode(t, err, oops.CodeForbidden)
	}
	_, err := ti.service.Get(asSupport, &gen.GetPayload{SessionToken: nil})
	require.NoError(t, err, "support can still read the issuer")
}

func TestMutations_RefuseLegacyImpersonatedSessions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	admin := adminCtx(t, ctx)
	kid := enable(t, ti, admin)
	authCtx, ok := contextvalues.GetAuthContext(admin)
	require.True(t, ok)
	impersonated := contextvalues.WithValidatedGramSession(admin, authCtx, true)

	for _, enabled := range []bool{true, false} {
		_, err := ti.service.SetEnabled(impersonated, &gen.SetEnabledPayload{Enabled: enabled, SessionToken: nil})
		requireOopsCode(t, err, oops.CodeForbidden)
	}
	for _, compromised := range []bool{true, false} {
		_, err := ti.service.Rotate(impersonated, &gen.RotatePayload{ActiveKid: &kid, Compromised: compromised, SessionToken: nil})
		requireOopsCode(t, err, oops.CodeForbidden)
	}
	_, err := ti.service.Get(impersonated, &gen.GetPayload{SessionToken: nil})
	require.NoError(t, err, "an impersonated session can still read the issuer")
}

func TestSetEnabled_EnablingRequiresRolloutAndVerifiedDomain(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	admin := adminCtx(t, ctx)

	ti.flags.SetFlag(feature.FlagInternalSessionIssuer, ti.orgID, false)
	_, err := ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: true, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	ti.flags.SetFlag(feature.FlagInternalSessionIssuer, ti.orgID, true)

	setVerifiedDomains(t, ctx, ti)
	_, err = ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: true, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSetEnabled_Lifecycle(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	admin := adminCtx(t, ctx)

	enabled, err := ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: true, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "enabled", enabled.State)
	require.Equal(t, wantIssuer(ti), *enabled.Issuer)
	again, err := ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: true, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, enabled.Keys, again.Keys, "enabling is idempotent")

	// Turning signing off never depends on the rollout or verified domains.
	ti.flags.SetFlag(feature.FlagInternalSessionIssuer, ti.orgID, false)
	setVerifiedDomains(t, ctx, ti)
	disabled, err := ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: false, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "disabled", disabled.State)
	require.Equal(t, wantIssuer(ti), *disabled.Issuer, "the issuer URI survives disabling")
	require.Len(t, disabled.Keys, 1, "keys stay published while disabled")

	ti.flags.SetFlag(feature.FlagInternalSessionIssuer, ti.orgID, true)
	setVerifiedDomains(t, ctx, ti, "example.com")
	reenabled, err := ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: true, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, enabled.Keys, reenabled.Keys, "re-enabling keeps the key")

	for _, tc := range []struct {
		action audit.Action
		want   int64
	}{{audit.ActionInternalSessionIssuerEnable, 2}, {audit.ActionInternalSessionIssuerDisable, 1}} {
		count, err := audittest.AuditLogCountByAction(ctx, ti.conn, tc.action)
		require.NoError(t, err)
		require.Equal(t, tc.want, count, tc.action)
	}
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionInternalSessionIssuerDisable)
	require.NoError(t, err)
	require.Equal(t, ti.orgID, record.OrganizationID)
}

func TestSetEnabled_DisablingNothingIsNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	_, err := ti.service.SetEnabled(adminCtx(t, ctx), &gen.SetEnabledPayload{Enabled: false, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestSetEnabled_UnavailableWithoutProvisioning(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, false)
	_, err := ti.service.SetEnabled(adminCtx(t, ctx), &gen.SetEnabledPayload{Enabled: true, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeUnavailable)
}

func TestSetEnabled_VerifiedDomainSufficesWithoutSSO(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	admin := adminCtx(t, ctx)

	setVerifiedDomains(t, ctx, ti)
	_, err := ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: true, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	// The fixture never grants the SSO entitlement; a verified domain is enough.
	setVerifiedDomains(t, ctx, ti, "example.com")
	view, err := ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: true, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "enabled", view.State)

	setVerifiedDomains(t, ctx, ti)
	disabled, err := ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: false, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "disabled", disabled.State, "an existing issuer stays disableable without a verified domain")
}
