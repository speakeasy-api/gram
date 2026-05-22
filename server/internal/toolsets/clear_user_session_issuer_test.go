package toolsets_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersessionsRepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestClearUserSessionIssuer_UnlinksAttachedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createMinimalPrivateToolset(t, ctx, ti, "clear-usi-linked")

	usi, err := usersessionsRepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsRepo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		Slug:               "test-usi-linked",
		AuthnChallengeMode: "none",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)

	_, err = toolsetsRepo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsetsRepo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: usi.ID, Valid: true},
		Slug:                string(toolset.Slug),
		ProjectID:           *authCtx.ProjectID,
	})
	require.NoError(t, err)

	cleared, err := ti.service.ClearUserSessionIssuer(ctx, &gen.ClearUserSessionIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             toolset.Slug,
	})
	require.NoError(t, err)
	require.NotNil(t, cleared)
	require.Nil(t, cleared.UserSessionIssuerID)
}

func TestClearUserSessionIssuer_NoopWhenAlreadyClear(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "clear-usi-noop")

	cleared, err := ti.service.ClearUserSessionIssuer(ctx, &gen.ClearUserSessionIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             toolset.Slug,
	})
	require.NoError(t, err)
	require.NotNil(t, cleared)
	require.Nil(t, cleared.UserSessionIssuerID)
}

func TestClearUserSessionIssuer_DeniedWithoutWriteScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "clear-usi-denied")

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeMCPRead, Selector: authz.NewSelector(authz.ScopeMCPRead, toolset.ID)})

	_, err := ti.service.ClearUserSessionIssuer(ctx, &gen.ClearUserSessionIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             toolset.Slug,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestClearUserSessionIssuer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	_, err := ti.service.ClearUserSessionIssuer(ctx, &gen.ClearUserSessionIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             "does-not-exist",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}
