package customdomains_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/domains"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestUpdateDomain_Activated_PersistsThenReconciles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)

	created, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "update-active.example.com",
		IngressName:     pgTextValid("ingress-active"),
		CertSecretName:  pgTextValid("cert-active"),
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	// Mark the domain activated so the edit flow re-applies to k8s.
	_, err = ti.repo.UpdateCustomDomain(ctx, cdrepo.UpdateCustomDomainParams{
		Verified:        true,
		Activated:       true,
		IngressName:     pgTextValid("ingress-active"),
		CertSecretName:  pgTextValid("cert-active"),
		ProvisionerKind: "ingress",
		ID:              created.ID,
	})
	require.NoError(t, err)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	res, err := ti.service.UpdateDomain(ctx, &gen.UpdateDomainPayload{
		SessionToken:             nil,
		IPAllowlist:              []string{"1.2.3.4", "10.0.0.0/8"},
		OpenaiAppsChallengeToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"1.2.3.4", "10.0.0.0/8"}, res.IPAllowlist)

	require.Equal(t, 1, ti.temporal.reconcileCalls)
	require.Equal(t, created.ID, ti.temporal.lastReconcileID)

	row, err := ti.repo.GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, []string{"1.2.3.4", "10.0.0.0/8"}, row.IpAllowlist)
}

func TestUpdateDomain_NotActivated_ReconcilesRegistrationDesiredState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)

	created, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "update-pending.example.com",
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	res, err := ti.service.UpdateDomain(ctx, &gen.UpdateDomainPayload{
		SessionToken:             nil,
		IPAllowlist:              []string{"1.2.3.4"},
		OpenaiAppsChallengeToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"1.2.3.4"}, res.IPAllowlist)

	require.Equal(t, 1, ti.temporal.reconcileCalls)
	require.Equal(t, created.ID, ti.temporal.lastReconcileID)
}

func TestUpdateDomain_OpenAIAppsChallengeTokenSetReplaceClearAndAudit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	_, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "challenge-token.example.com",
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{"10.0.0.0/8"},
	})
	require.NoError(t, err)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))

	first := "challenge-token-first"
	res, err := ti.service.UpdateDomain(ctx, &gen.UpdateDomainPayload{
		SessionToken:             nil,
		IPAllowlist:              nil,
		OpenaiAppsChallengeToken: &first,
	})
	require.NoError(t, err)
	require.Equal(t, first, requireValue(t, res.OpenaiAppsChallengeToken))
	require.Equal(t, []string{"10.0.0.0/8"}, res.IPAllowlist)
	require.Equal(t, 1, ti.temporal.reconcileCalls)
	requireLatestChallengeTokenAuditTransition(t, ctx, ti, nil, &first)

	second := "challenge-token-second"
	res, err = ti.service.UpdateDomain(ctx, &gen.UpdateDomainPayload{
		SessionToken:             nil,
		IPAllowlist:              nil,
		OpenaiAppsChallengeToken: &second,
	})
	require.NoError(t, err)
	require.Equal(t, second, requireValue(t, res.OpenaiAppsChallengeToken))
	require.Equal(t, 2, ti.temporal.reconcileCalls)
	requireLatestChallengeTokenAuditTransition(t, ctx, ti, &first, &second)

	clearToken := ""
	res, err = ti.service.UpdateDomain(ctx, &gen.UpdateDomainPayload{
		SessionToken:             nil,
		IPAllowlist:              nil,
		OpenaiAppsChallengeToken: &clearToken,
	})
	require.NoError(t, err)
	require.Nil(t, res.OpenaiAppsChallengeToken)
	require.Equal(t, 3, ti.temporal.reconcileCalls)
	requireLatestChallengeTokenAuditTransition(t, ctx, ti, &second, nil)

	row, err := ti.repo.GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.False(t, row.OpenaiAppsChallengeToken.Valid)
	require.Equal(t, []string{"10.0.0.0/8"}, row.IpAllowlist)
}

func TestUpdateDomain_OpenAIAppsChallengeTokenValidation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	_, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "challenge-validation.example.com",
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))

	tooLong := strings.Repeat("a", 257)
	_, err = ti.service.UpdateDomain(ctx, &gen.UpdateDomainPayload{
		SessionToken:             nil,
		IPAllowlist:              nil,
		OpenaiAppsChallengeToken: &tooLong,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	multiline := "line-one\nline-two"
	_, err = ti.service.UpdateDomain(ctx, &gen.UpdateDomainPayload{
		SessionToken:             nil,
		IPAllowlist:              nil,
		OpenaiAppsChallengeToken: &multiline,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCustomDomainOpenAIAppsChallengeTokenDatabaseConstraint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	_, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "challenge-constraint.example.com",
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	for _, invalidToken := range []string{"", strings.Repeat("a", 257), "line-one\nline-two", "line-one\rline-two"} {
		_, err = ti.repo.UpdateCustomDomainSettings(ctx, cdrepo.UpdateCustomDomainSettingsParams{
			UpdateIpAllowlist:              false,
			IpAllowlist:                    []string{},
			UpdateOpenaiAppsChallengeToken: true,
			OpenaiAppsChallengeToken:       pgtype.Text{String: invalidToken, Valid: true},
			OrganizationID:                 authCtx.ActiveOrganizationID,
		})
		require.ErrorContains(t, err, "custom_domains_openai_apps_challenge_token_check")
	}
}

func TestCustomDomainOpenAIAppsChallengeTokenDatabaseBoundsAndNull(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	_, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "challenge-bounds.example.com",
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	maxToken := strings.Repeat("a", 256)
	row, err := ti.repo.UpdateCustomDomainSettings(ctx, cdrepo.UpdateCustomDomainSettingsParams{
		UpdateIpAllowlist:              false,
		IpAllowlist:                    []string{},
		UpdateOpenaiAppsChallengeToken: true,
		OpenaiAppsChallengeToken:       pgtype.Text{String: maxToken, Valid: true},
		OrganizationID:                 authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	require.Equal(t, maxToken, row.OpenaiAppsChallengeToken.String)

	row, err = ti.repo.UpdateCustomDomainSettings(ctx, cdrepo.UpdateCustomDomainSettingsParams{
		UpdateIpAllowlist:              false,
		IpAllowlist:                    []string{},
		UpdateOpenaiAppsChallengeToken: true,
		OpenaiAppsChallengeToken:       pgtype.Text{String: "", Valid: false},
		OrganizationID:                 authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	require.False(t, row.OpenaiAppsChallengeToken.Valid)
}

func requireLatestChallengeTokenAuditTransition(t *testing.T, ctx context.Context, ti *serviceTestInstance, before, after *string) {
	t.Helper()

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	if before == nil {
		require.Nil(t, beforeSnapshot["OpenaiAppsChallengeToken"])
	} else {
		require.Equal(t, *before, beforeSnapshot["OpenaiAppsChallengeToken"])
	}
	if after == nil {
		require.Nil(t, afterSnapshot["OpenaiAppsChallengeToken"])
	} else {
		require.Equal(t, *after, afterSnapshot["OpenaiAppsChallengeToken"])
	}
}
