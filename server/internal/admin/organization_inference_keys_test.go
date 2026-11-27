package admin

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// newOrganizationKeyService returns an admin service whose organization key
// sync runs the production OpenRouter client against a recording upstream, and
// an enabled organization holding one classified, enabled key of each type.
func newOrganizationKeyService(t *testing.T, orgID string) (context.Context, *Service, *pgxpool.Pool, *rearmUpstream) {
	t.Helper()

	ctx, svc, conn, upstream := newProductionRearmService(t)
	keys, ok := svc.openRouter.(OrganizationInferenceKeys)
	require.True(t, ok, "the production OpenRouter client must serve organization key sync")
	svc.organizationKeys = keys

	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID + " Name", slug: orgID + "-slug", accountType: "enterprise", whitelisted: true})
	for _, keyType := range openrouter.AllKeyTypes {
		seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: keyType, monthlyCredits: 50, disabled: false})
		classifyRearmKey(t, ctx, conn, orgID, keyType, []string{})
	}

	return ctx, svc, conn, upstream
}

func TestDisableOrganization_LocksInferenceKeysUntilEnabled(t *testing.T) {
	t.Parallel()

	const orgID = "org_keys_round_trip"
	ctx, svc, conn, upstream := newOrganizationKeyService(t, orgID)

	_, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: orgID})
	require.NoError(t, err)

	for _, keyType := range openrouter.AllKeyTypes {
		row := readOpenRouterKey(t, ctx, conn, orgID, keyType)
		require.Equal(t, []string{string(openrouter.DisableCauseOrganizationDisabled)}, row.DisableCauses, "%s key", keyType)
		require.True(t, row.Disabled, "%s key", keyType)
	}
	require.Equal(t, len(openrouter.AllKeyTypes), upstream.count(), "each key is disabled upstream once")

	_, err = svc.EnableOrganization(ctx, &gen.EnableOrganizationPayload{ID: orgID})
	require.NoError(t, err)

	for _, keyType := range openrouter.AllKeyTypes {
		row := readOpenRouterKey(t, ctx, conn, orgID, keyType)
		require.Empty(t, row.DisableCauses, "%s key", keyType)
		require.False(t, row.Disabled, "%s key", keyType)
		require.EqualValues(t, 50, row.MonthlyCredits, "enabling keeps the stored limit for the %s key", keyType)
	}
	require.Equal(t, 2*len(openrouter.AllKeyTypes), upstream.count(), "each key is enabled upstream once")
}

func TestDisableOrganization_RepeatedDisableLeavesKeysAlone(t *testing.T) {
	t.Parallel()

	const orgID = "org_keys_repeat"
	ctx, svc, conn, upstream := newOrganizationKeyService(t, orgID)

	_, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: orgID})
	require.NoError(t, err)
	_, err = svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: orgID})
	require.NoError(t, err)

	require.Equal(t, len(openrouter.AllKeyTypes), upstream.count(), "a key already holding the cause is not sent upstream again")
	for _, keyType := range openrouter.AllKeyTypes {
		require.Equal(t, []string{string(openrouter.DisableCauseOrganizationDisabled)}, readOpenRouterKey(t, ctx, conn, orgID, keyType).DisableCauses)
	}
}

func TestEnableOrganization_KeepsOtherDisableCauses(t *testing.T) {
	t.Parallel()

	const orgID = "org_keys_layered"
	ctx, svc, conn, _ := newOrganizationKeyService(t, orgID)
	classifyRearmKey(t, ctx, conn, orgID, openrouter.KeyTypeChat, []string{string(openrouter.DisableCauseAdminLock)})

	_, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: orgID})
	require.NoError(t, err)
	require.Equal(t,
		[]string{string(openrouter.DisableCauseAdminLock), string(openrouter.DisableCauseOrganizationDisabled)},
		readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat).DisableCauses,
	)

	_, err = svc.EnableOrganization(ctx, &gen.EnableOrganizationPayload{ID: orgID})
	require.NoError(t, err)

	chat := readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat)
	require.Equal(t, []string{string(openrouter.DisableCauseAdminLock)}, chat.DisableCauses, "a staff lock set before the disable survives the enable")
	require.True(t, chat.Disabled)

	internal := readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeInternal)
	require.Empty(t, internal.DisableCauses)
	require.False(t, internal.Disabled)
}

func TestDisableOrganization_StandsWhenOpenRouterFailsAndRetryLocksKeys(t *testing.T) {
	t.Parallel()

	const orgID = "org_keys_upstream_down"
	ctx, svc, conn, upstream := newOrganizationKeyService(t, orgID)
	upstream.setFail(true)

	res, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: orgID})
	require.NoError(t, err, "an OpenRouter failure must not fail the disable")
	require.NotNil(t, res.DisabledAt)
	require.NotNil(t, readDisabledAt(t, ctx, conn, orgID))
	for _, keyType := range openrouter.AllKeyTypes {
		require.Empty(t, readOpenRouterKey(t, ctx, conn, orgID, keyType).DisableCauses, "the cause is recorded only once the %s key is disabled upstream", keyType)
	}

	upstream.setFail(false)
	_, err = svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: orgID})
	require.NoError(t, err)
	for _, keyType := range openrouter.AllKeyTypes {
		require.Equal(t, []string{string(openrouter.DisableCauseOrganizationDisabled)}, readOpenRouterKey(t, ctx, conn, orgID, keyType).DisableCauses, "repeating the disable locks the %s key", keyType)
	}
}

func TestRepairInferenceKey_RefusesOrganizationDisabledCauseWhileDisabled(t *testing.T) {
	t.Parallel()

	const orgID = "org_keys_repair_refused"
	ctx, svc, conn, _ := newOrganizationKeyService(t, orgID)

	_, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: orgID})
	require.NoError(t, err)

	staffCtx := contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	_, err = svc.RepairInferenceKey(staffCtx, &gen.RepairInferenceKeyPayload{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeChat),
		RemoveCauses:   []string{string(openrouter.DisableCauseOrganizationDisabled)},
		Confirmation:   "I know what I'm doing",
		Reason:         "checking the disabled organization gate",
	})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Equal(t,
		[]string{string(openrouter.DisableCauseOrganizationDisabled)},
		readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat).DisableCauses,
	)
}
