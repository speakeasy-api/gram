package identityapi_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/identity"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	workosRepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
)

// The whole point of the identity URN is that every surface links with the
// identifier it happens to hold. Whichever one that is, the caller must land
// on the same subject and the same canonical URN.
func TestResolve_ConvergesOnCanonicalURN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestIdentityService(t)

	byID, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: "user:" + mockidp.MockUserID, ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)

	byEmail, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: "email:" + mockidp.MockUserEmail, ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)

	require.Equal(t, "user:"+mockidp.MockUserID, byID.CanonicalUrn)
	require.Equal(t, byID.CanonicalUrn, byEmail.CanonicalUrn)
	require.Equal(t, byID.UserIds, byEmail.UserIds)
	require.Equal(t, byID.Emails, byEmail.Emails)
}

func TestResolve_DirectoryUser(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestIdentityService(t)

	resolved, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: "user:" + mockidp.MockUserID, ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)

	require.Equal(t, "human", resolved.Kind)
	require.Equal(t, "Dev User", resolved.DisplayName)
	require.Equal(t, []string{mockidp.MockUserID}, resolved.UserIds)
	require.Contains(t, resolved.Emails, mockidp.MockUserEmail)

	// Every address the person is known by is a candidate external user id,
	// because agents report whichever address the local tool was configured
	// with.
	require.Contains(t, resolved.ExternalUserIds, mockidp.MockUserEmail)
}

// An email that matches no directory row still has telemetry and cost, so it
// resolves to an unattributed identity rather than failing.
func TestResolve_UnknownEmailIsUnattributed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestIdentityService(t)

	resolved, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: "email:nobody@example.com", ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)

	require.Equal(t, "unattributed", resolved.Kind)
	require.Equal(t, "email:nobody@example.com", resolved.CanonicalUrn)
	require.Empty(t, resolved.UserIds)
	require.Equal(t, []string{"nobody@example.com"}, resolved.Emails)
}

// Agents commonly report the person's address as the external user id, so an
// address-shaped external URN resolves to the person behind it.
func TestResolve_ExternalUserID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestIdentityService(t)

	opaque, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: "external:svc-7", ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "unattributed", opaque.Kind)
	require.Equal(t, "external:svc-7", opaque.CanonicalUrn)
	require.Equal(t, []string{"svc-7"}, opaque.ExternalUserIds)

	addressShaped, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: "external:" + mockidp.MockUserEmail, ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "human", addressShaped.Kind)
	require.Equal(t, "user:"+mockidp.MockUserID, addressShaped.CanonicalUrn)
}

func TestResolve_APIKeySubject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestIdentityService(t)

	resolved, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: "apikey:33333333-3333-3333-3333-333333333333", ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)

	require.Equal(t, "apikey", resolved.Kind)
	require.Equal(t, "apikey:33333333-3333-3333-3333-333333333333", resolved.CanonicalUrn)
	require.Empty(t, resolved.UserIds)
}

func TestResolve_RejectsInvalidAndUnsupportedURNs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestIdentityService(t)

	tests := []struct {
		name string
		urn  string
		code oops.Code
	}{
		{name: "malformed", urn: "not-a-urn", code: oops.CodeBadRequest},
		{name: "role principal", urn: "role:organization:admin", code: oops.CodeBadRequest},
		{name: "agent not yet minted", urn: "agent:agt_01abc", code: oops.CodeNotImplemented},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: tt.urn, ApikeyToken: nil, SessionToken: nil})
			require.Error(t, err)

			var shareable *oops.ShareableError
			require.ErrorAs(t, err, &shareable)
			require.Equal(t, tt.code, shareable.Code)
		})
	}
}

// seedDirectoryUser records the Directory Sync row and attributes WorkOS would
// have synced for a member, through the same upsert the directory event
// activity uses.
func seedDirectoryUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, userID, email string, attributes string) {
	t.Helper()

	now := time.Now().UTC()
	_, err := workosRepo.New(conn).UpsertDirectoryUser(ctx, workosRepo.UpsertDirectoryUserParams{
		OrganizationID:        orgID,
		UserID:                conv.ToPGText(userID),
		WorkosDirectoryUserID: "directory_user_test",
		Email:                 conv.ToPGText(email),
		Attributes:            []byte(attributes),
		WorkosCreatedAt:       conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(now),
		WorkosLastEventID:     conv.ToPGText("event_test"),
		RestoreDeleted:        false,
	})
	require.NoError(t, err)
}

// Directory attributes are the one part of the identity with no endpoint of
// its own, so resolution is the only thing that can surface them.
func TestResolve_DirectoryAttributes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestIdentityService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedDirectoryUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockidp.MockUserID, mockidp.MockUserEmail,
		`{"department_name":"Engineering","job_title":"  Staff Engineer  ","employee_type":"","division_name":"R&D","cost_center_name":"CC-1"}`)

	resolved, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: "user:" + mockidp.MockUserID, ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)

	require.NotNil(t, resolved.Directory)
	require.Equal(t, "Engineering", conv.PtrValOrEmpty(resolved.Directory.DepartmentName, ""))
	require.Equal(t, "R&D", conv.PtrValOrEmpty(resolved.Directory.DivisionName, ""))
	require.Equal(t, "CC-1", conv.PtrValOrEmpty(resolved.Directory.CostCenterName, ""))

	// IdP mappings are customer-controlled, so values are trimmed and anything
	// blank is reported as absent rather than as an empty string.
	require.Equal(t, "Staff Engineer", conv.PtrValOrEmpty(resolved.Directory.JobTitle, ""))
	require.Nil(t, resolved.Directory.EmployeeType)
}

// A user id nobody owns must not come back carrying that id: the kind says
// there is no directory user, and a user id alongside it would have the client
// query user-keyed subsystems for a subject that has no rows in them.
func TestResolve_UnknownUserIDCarriesNoUserID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestIdentityService(t)

	resolved, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: "user:user_nobody", ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)

	require.Equal(t, "unattributed", resolved.Kind)
	require.Empty(t, resolved.UserIds)
	require.Equal(t, "user:user_nobody", resolved.CanonicalUrn)
	require.Equal(t, "user_nobody", resolved.DisplayName)
}
