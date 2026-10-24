package platformmcp

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestAHPProjectsLiveMemberGrants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "ahp_member_admission")
	require.NoError(t, err)
	principal, allowedProject := seedRegistrationLifecycle(t, ctx, conn)
	seedPlatformMCPAuthorizationMember(t, ctx, conn, principal.OrganizationID, principal.UserID, authz.SystemRoleMember)
	deniedProject, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Denied diagnostics project", Slug: "denied-diagnostics-" + uuid.NewString()[:8], OrganizationID: principal.OrganizationID,
	})
	require.NoError(t, err)

	role := seedAccessRole(t, ctx, conn, principal.OrganizationID, "diagnostics-reader", "Diagnostics Reader")
	_, err = accessrepo.New(conn).UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID: principal.OrganizationID, WorkosUserID: "workos-" + principal.UserID, WorkosRoleSlug: "diagnostics-reader",
		UserID: conv.ToPGText(principal.UserID), WorkosMembershipID: conv.ToPGText("membership-" + principal.UserID), WorkosUpdatedAt: conv.ToPGTimestamptz(time.Now().UTC()), WorkosLastEventID: pgtype.Text{},
	})
	require.NoError(t, err)
	selector := authz.NewSelector(authz.ScopeProjectRead, allowedProject.ID.String())
	encoded, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: principal.OrganizationID,
		PrincipalUrn:   role,
		Scope:          string(authz.ScopeProjectRead),
		Selectors:      encoded,
	})
	require.NoError(t, err)

	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	admission := NewAHPProjects(conn, engine)
	otherDomainCtx := requestorigin.WithContext(ctx, requestorigin.Origin{Surface: requestorigin.SurfaceCustomDomain, BaseURL: "https://other.example.test", OrganizationID: "other-org"})
	_, err = admission.EligibleProjects(otherDomainCtx, principal)
	require.ErrorIs(t, err, ErrForbidden)
	_, err = admission.AuthorizeProject(otherDomainCtx, principal, allowedProject.ID.String())
	require.ErrorIs(t, err, ErrForbidden)
	options, err := admission.EligibleProjects(ctx, principal)
	require.NoError(t, err)
	require.Equal(t, []AHPProjectOption{{ID: allowedProject.ID.String(), Name: allowedProject.Name, Slug: allowedProject.Slug}}, options)
	prepared, err := admission.AuthorizeProject(ctx, principal, allowedProject.ID.String())
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(prepared)
	require.True(t, ok)
	require.Equal(t, principal.UserID, authCtx.UserID)
	require.Equal(t, allowedProject.ID, *authCtx.ProjectID)
	_, err = admission.AuthorizeProject(ctx, principal, deniedProject.ID.String())
	require.ErrorIs(t, err, ErrForbidden)
	for _, unavailableProject := range []string{"invalid", uuid.NewString()} {
		_, err = admission.AuthorizeProject(ctx, principal, unavailableProject)
		require.ErrorIs(t, err, ErrForbidden)
	}
	// The eligible-project picker reads every page, not only the first 100 rows.
	for range 101 {
		_, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Pagination project", Slug: "page-" + uuid.NewString()[:8], OrganizationID: principal.OrganizationID})
		require.NoError(t, err)
	}
	allProjects, err := authz.NewSelector(authz.ScopeProjectRead, authz.WildcardResource).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: principal.OrganizationID, PrincipalUrn: role, Scope: string(authz.ScopeProjectRead), Selectors: allProjects})
	require.NoError(t, err)
	options, err = admission.EligibleProjects(ctx, principal)
	require.NoError(t, err)
	require.Len(t, options, 103)
	// Removing the grant invalidates admission even with an inherited authorized context.
	_, err = accessrepo.New(conn).DeletePrincipalGrantsByPrincipal(ctx, accessrepo.DeletePrincipalGrantsByPrincipalParams{OrganizationID: principal.OrganizationID, PrincipalUrn: role})
	require.NoError(t, err)
	options, err = admission.EligibleProjects(prepared, principal)
	require.NoError(t, err)
	require.Empty(t, options)
	_, err = admission.AuthorizeProject(prepared, principal, allowedProject.ID.String())
	require.Error(t, err)
	require.NoError(t, organizationsrepo.New(conn).DeleteOrganizationUserRelationship(ctx, organizationsrepo.DeleteOrganizationUserRelationshipParams{OrganizationID: principal.OrganizationID, UserID: conv.ToPGText(principal.UserID)}))
	_, err = admission.EligibleProjects(prepared, principal)
	require.ErrorIs(t, err, ErrForbidden)
}

type ahpAdmissionSessionStore struct {
	session oauth.Session
	err     error
	calls   int
}

func (s *ahpAdmissionSessionStore) GetActiveSession(context.Context, string, string) (oauth.Session, error) {
	s.calls++
	return s.session, s.err
}

type ahpAdmissionProjects struct {
	projectID string
	err       error
}

func (*ahpAdmissionProjects) EligibleProjects(context.Context, Principal) ([]AHPProjectOption, error) {
	return nil, nil
}
func (p *ahpAdmissionProjects) AuthorizeProject(ctx context.Context, principal Principal, projectID string) (context.Context, error) {
	p.projectID = projectID
	if p.err != nil {
		return ctx, p.err
	}
	id := uuid.MustParse(projectID)
	slug := "selected"
	return contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{UserID: principal.UserID, ActiveOrganizationID: principal.OrganizationID, ProjectID: &id, ProjectSlug: &slug}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID)), nil
}

func TestAHPAuthenticatorBoundProjectAndAudience(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, audience, issuer, header, originOrg string
		storeErr, admissionErr, expected          error
	}{
		{name: "bound project", audience: "/ahp", issuer: "/ahp"},
		{name: "custom domain matching organization", audience: "/ahp", issuer: "/ahp", originOrg: "test-org"},
		{name: "custom domain different organization", audience: "/ahp", issuer: "/ahp", originOrg: "other-org", expected: ErrForbidden},
		{name: "matching header", audience: "/ahp", issuer: "/ahp", header: "selected"},
		{name: "header cannot override", audience: "/ahp", issuer: "/ahp", header: "other", expected: ErrForbidden},
		{name: "platform audience", audience: "/platform-mcp", issuer: "/ahp", expected: ErrUnauthorized},
		{name: "platform issuer", audience: "/ahp", issuer: "/platform-mcp", expected: ErrUnauthorized},
		{name: "revoked session", audience: "/ahp", issuer: "/ahp", storeErr: oauth.ErrRevoked, expected: ErrUnauthorized},
		{name: "session outage", audience: "/ahp", issuer: "/ahp", storeErr: errors.New("unavailable"), expected: ErrUnavailable},
		{name: "revoked membership", audience: "/ahp", issuer: "/ahp", admissionErr: ErrForbidden, expected: ErrForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base, err := url.Parse("https://api.example.test")
			require.NoError(t, err)
			encryptionClient, err := encryption.NewWithBytes(make([]byte, 32))
			require.NoError(t, err)
			subject := urn.NewUserSubject("test-user")
			projectID := uuid.NewString()
			store := &ahpAdmissionSessionStore{session: oauth.Session{ClientID: "test-client", Connection: oauth.Connection{ID: uuid.NewString(), ClientID: "test-client", Subject: subject.String(), OrganizationID: "test-org", Generation: uuid.NewString(), ProjectID: projectID, ResourceURI: base.String() + "/ahp"}}, err: tc.storeErr}
			projects := &ahpAdmissionProjects{err: tc.admissionErr}
			signer := sessiontokens.NewSigner("test-signing-key")
			authenticator, err := NewAHPAuthenticator(signer, store, encryptionClient, base, projects)
			require.NoError(t, err)
			jti, err := authenticator.credentials.Issue(accessJTICredential, "test-org", base.String()+"/ahp")
			require.NoError(t, err)
			token, _, err := signer.Mint(sessiontokens.MintParams{Subject: subject, Audience: base.String() + tc.audience, Issuer: base.String() + tc.issuer, Lifetime: time.Hour, ClientID: "test-client", JTI: jti})
			require.NoError(t, err)
			requestCtx := t.Context()
			if tc.originOrg != "" {
				requestCtx = requestorigin.WithContext(requestCtx, requestorigin.Origin{Surface: requestorigin.SurfaceCustomDomain, BaseURL: base.String(), OrganizationID: tc.originOrg})
			}
			ctx, err := authenticator.AuthenticateAHP(requestCtx, token, tc.header)
			if tc.expected != nil {
				require.ErrorIs(t, err, tc.expected)
				return
			}
			require.NoError(t, err)
			require.Equal(t, projectID, projects.projectID)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.Equal(t, projectID, authCtx.ProjectID.String())
		})
	}
}
