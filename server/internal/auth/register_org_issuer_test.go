package auth_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	orgid "github.com/speakeasy-api/gram/server/internal/organizations/id"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func organizationIssuers(t *testing.T, ctx context.Context, inst *e2eInstance, organizationID string) []usersessionsrepo.ListOrganizationUserSessionIssuersRow {
	t.Helper()
	rows, err := usersessionsrepo.New(inst.conn).ListOrganizationUserSessionIssuers(ctx, usersessionsrepo.ListOrganizationUserSessionIssuersParams{
		OrganizationID: organizationID, IncludeOrganizational: true, IncludeProjectSpecific: false,
		Cursor: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, LimitValue: 10,
	})
	require.NoError(t, err)
	return rows
}

// Signup no longer creates an organization-level issuer: new MCP servers
// default to one, which binds every upstream client to it.
func TestRegisterCreatesNoOrganizationIssuer(t *testing.T) {
	t.Parallel()
	const name = "Issuer Signup Fixture"
	fetcher := &mockWorkOSFetcher{members: map[string][]workos.Member{}, orgs: map[string]*workos.Organization{}}
	info := &MockUserInfo{UserID: "user_issuer_signup_fixture", Email: "issuer-signup@example.test", Organizations: []MockOrganizationEntry{}}
	ctx, inst := newE2EAuthService(t, info, fetcher)
	callback, err := inst.callbackWithNonce(ctx, t)
	require.NoError(t, err)
	ctx, err = inst.sessionManager.Authenticate(ctx, callback.SessionToken)
	require.NoError(t, err)
	before, err := audittest.AuditLogCountByAction(ctx, inst.conn, audit.ActionUserSessionIssuerCreate)
	require.NoError(t, err)
	require.NoError(t, inst.service.Register(ctx, &gen.RegisterPayload{OrgName: name}))
	organizationID := orgid.FromWorkOSID("workos_org_" + name)
	_, err = orgrepo.New(inst.conn).GetOrganizationMetadata(ctx, organizationID)
	require.NoError(t, err)
	require.Empty(t, organizationIssuers(t, ctx, inst, organizationID))
	after, err := audittest.AuditLogCountByAction(ctx, inst.conn, audit.ActionUserSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
