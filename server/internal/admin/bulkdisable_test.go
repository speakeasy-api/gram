package admin

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestBulkDisableOrganizations_DisablesAndUnwhitelists(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_bd1", name: "BD One", slug: "bd-one", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_bd2", name: "BD Two", slug: "bd-two", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_bd_other", name: "Bystander", slug: "bystander", whitelisted: true})

	res, err := svc.BulkDisableOrganizations(ctx, &gen.BulkDisableOrganizationsPayload{
		Ids: []string{"org_bd2", "org_missing", "org_bd1", "org_bd1"},
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"org_bd1", "org_bd2"}, res.DisabledIds)
	require.Equal(t, []string{"org_missing"}, res.MissingIds)

	for _, id := range []string{"org_bd1", "org_bd2"} {
		state := readOrgState(t, ctx, conn, id)
		require.True(t, state.DisabledAt.Valid, "%s must be disabled", id)
		require.False(t, state.Whitelisted, "%s must be removed from the whitelist", id)
	}
	bystander := readOrgState(t, ctx, conn, "org_bd_other")
	require.False(t, bystander.DisabledAt.Valid, "an organization that was not named must be untouched")
	require.True(t, bystander.Whitelisted)

	disabledEvents, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationDisabled)
	require.NoError(t, err)
	require.Equal(t, int64(2), disabledEvents)
	whitelistEvents, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationWhitelistUpdated)
	require.NoError(t, err)
	require.Equal(t, int64(2), whitelistEvents)
}

func TestBulkDisableOrganizations_AlreadyDisabledKeepsTimestamp(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	original := time.Date(2026, time.February, 2, 3, 4, 5, 0, time.UTC)
	// Disabled but still whitelisted: the bulk disable must finish the job
	// without moving the original disable time.
	seedOrg(t, ctx, conn, orgFixture{id: "org_bd_old", name: "Old", slug: "old", whitelisted: true, disabledAt: &original})

	res, err := svc.BulkDisableOrganizations(ctx, &gen.BulkDisableOrganizationsPayload{Ids: []string{"org_bd_old"}})
	require.NoError(t, err)
	require.Equal(t, []string{"org_bd_old"}, res.DisabledIds)

	state := readOrgState(t, ctx, conn, "org_bd_old")
	require.True(t, state.DisabledAt.Valid)
	require.True(t, original.Equal(state.DisabledAt.Time), "disabled_at moved: want %s, got %s", original, state.DisabledAt.Time)
	require.False(t, state.Whitelisted)

	disabledEvents, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationDisabled)
	require.NoError(t, err)
	require.Equal(t, int64(0), disabledEvents, "nothing changed about access, so no disable event")
}

func TestBulkDisableOrganizations_AuditFailureRollsBackEveryOrganization(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_bd_rb1", name: "RB One", slug: "rb-one", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_bd_rb2", name: "RB Two", slug: "rb-two", whitelisted: true})
	require.NoError(t, audittest.RejectAction(ctx, conn, audit.ActionOrganizationWhitelistUpdated))

	_, err := svc.BulkDisableOrganizations(ctx, &gen.BulkDisableOrganizationsPayload{Ids: []string{"org_bd_rb1", "org_bd_rb2"}})
	require.Error(t, err)

	for _, id := range []string{"org_bd_rb1", "org_bd_rb2"} {
		state := readOrgState(t, ctx, conn, id)
		require.False(t, state.DisabledAt.Valid, "%s must stay enabled when the batch fails", id)
		require.True(t, state.Whitelisted, "%s must stay whitelisted when the batch fails", id)
	}
}

func TestBulkDisableOrganizations_RejectsBadInput(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)

	_, err := svc.BulkDisableOrganizations(ctx, &gen.BulkDisableOrganizationsPayload{Ids: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = svc.BulkDisableOrganizations(ctx, &gen.BulkDisableOrganizationsPayload{Ids: []string{""}})
	requireOopsCode(t, err, oops.CodeBadRequest)

	tooMany := make([]string, constants.MaxBulkDisableOrganizations+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("org_%d", i)
	}
	_, err = svc.BulkDisableOrganizations(ctx, &gen.BulkDisableOrganizationsPayload{Ids: tooMany})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
