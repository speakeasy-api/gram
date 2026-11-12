package remotesessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// A client-to-provider foreign key alone does not enforce provider ownership.
// Neither a direct sign-in link nor an auth-client offer may make such a
// malformed registration eligible to send a stored refresh token upstream.
func TestRefreshSweepProviderScope(t *testing.T) {
	t.Parallel()
	for _, signInTier := range []string{"project", "organization"} {
		for _, reach := range []string{"direct", "offered"} {
			for _, scope := range []string{"same-project", "same-org", "global", "other-project", "foreign-org", "foreign-project"} {
				for _, operation := range []string{"claim", "recheck"} {
					t.Run(signInTier+"/"+reach+"/"+scope+"/"+operation, func(t *testing.T) {
						t.Parallel()
						ctx, ti := newTestService(t)
						auth, ok := contextvalues.GetAuthContext(ctx)
						require.True(t, ok)
						require.NotNil(t, auth.ProjectID)
						projectID, organization := *auth.ProjectID, auth.ActiveOrganizationID
						providerProject, providerOrg := conv.ToNullUUID(projectID), conv.ToPGText(organization)
						switch scope {
						case "same-org":
							providerProject = uuid.NullUUID{}
						case "global":
							providerProject, providerOrg = uuid.NullUUID{}, pgtype.Text{}
						case "other-project":
							providerProject = conv.ToNullUUID(createProject(t, ctx, ti.conn, "other-provider"))
						case "foreign-org", "foreign-project":
							providerProject = uuid.NullUUID{}
							_, err := testrepo.New(ti.conn).CreateAttachmentForeignOrganizationFixture(ctx)
							require.NoError(t, err)
							providerOrg = conv.ToPGText("org_attachment_other")
							if scope == "foreign-project" {
								id, err := testrepo.New(ti.conn).CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{
									ID: uuid.New(), Name: "foreign provider", Slug: "foreign-provider", OrganizationID: providerOrg.String,
								})
								require.NoError(t, err)
								providerProject = conv.ToNullUUID(id)
							}
						}
						providerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, providerProject, providerOrg, "provider-scope", "https://provider-scope.example.com")
						// Deliberately bypass the interactive writer's tenant validation: the
						// simple foreign key accepts every provider ownership tier above.
						clientID := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, projectID, providerID, "provider-scope-client")
						signIn := createUserSessionIssuer(t, ctx, ti.conn, "provider-scope-sign-in")
						if signInTier == "organization" {
							require.NoError(t, testrepo.New(ti.conn).SetUserSessionIssuerTenancyFixture(ctx, testrepo.SetUserSessionIssuerTenancyFixtureParams{
								ID: signIn, ProjectID: uuid.NullUUID{}, OrganizationID: conv.ToPGText(organization),
							}))
						}
						q := repo.New(ti.conn)
						if reach == "direct" {
							require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
								RemoteSessionClientID: clientID, UserSessionIssuerID: signIn,
							}))
						} else {
							serverID := seedRemoteMcpServerForLogin(t, ctx, ti.conn, projectID, signIn, "provider-scope", "https://provider-scope.example.com/mcp")
							offerRegistration(t, ctx, ti.conn, organization, projectID, serverID, clientID)
							enforceUpstreamAuth(t, ctx, ti.conn, organization)
						}
						o := offeredRegistration{ti: ti, projectID: projectID, organization: organization, signIn: signIn, registration: clientID, subject: urn.NewUserSubject("provider-scope-subject")}
						enableOrgAutoRefreshFeature(t, ctx, ti, organization, productfeatures.FeatureRemoteSessionAutoRefresh)
						sessionID := o.grant(t, ctx, signIn, true, true)
						seedGramSession(t, ctx, ti, o.subject, signIn, "provider-scope", 24*time.Hour)
						window := newSweepWindow()
						allowed := scope == "same-project" || scope == "same-org" || scope == "global"

						// Each operation has its own grant, so either can fail independently.
						if operation == "recheck" {
							candidate, err := q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(sessionID, organization))
							if !allowed {
								require.ErrorIs(t, err, pgx.ErrNoRows)
								return
							}
							require.NoError(t, err)
							require.Equal(t, sessionID, candidate.RemoteSession.ID)
							return
						}
						rows, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
						require.NoError(t, err)
						if !allowed {
							require.Empty(t, rows)
							return
						}
						require.Len(t, rows, 1)
						require.Equal(t, sessionID, rows[0].ID)
						require.Equal(t, organization, rows[0].OrganizationID)
					})
				}
			}
		}
	}
}
