package mcp_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	builtinrepo "github.com/speakeasy-api/gram/server/internal/builtinmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestBuiltinBinding_UnavailableOAuthPreservesKeyAuthorization(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"missing", "ambiguous-project", "ambiguous-organization", "deleted", "soft-deleted", "wrong-project", "wrong-organization"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			ac, _ := contextvalues.GetAuthContext(ctx)
			agentID, key := seedBuiltinAgent(t, ctx, ti, true)
			wrongAgentID, wrongKey := seedBuiltinAgent(t, ctx, ti, false)
			principalTokens := make([]string, 2)
			for i, id := range []uuid.UUID{agentID, wrongAgentID} {
				token, _, err := ti.principalCredentials.Mint(principalcredential.Credential{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, Principal: urn.NewPrincipal(urn.PrincipalTypeAgent, id.String()), AuthorizerUserID: ac.UserID})
				require.NoError(t, err)
				principalTokens[i] = token
			}
			projectRead, err := authz.NewSelector(authz.ScopeProjectRead, ac.ProjectID.String()).MarshalJSON()
			require.NoError(t, err)
			_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), Scope: string(authz.ScopeProjectRead), Selectors: projectRead})
			require.NoError(t, err)
			switch scenario {
			case "ambiguous-project", "ambiguous-organization":
				org := scenario == "ambiguous-organization"
				builtinIssuerFixture(t, ctx, ti, org)
				builtinIssuerFixture(t, ctx, ti, org)
				if !org {
					builtinIssuerFixture(t, ctx, ti, true)
				}
			case "deleted", "soft-deleted", "wrong-project", "wrong-organization":
				issuer := builtinIssuerFixture(t, ctx, ti, false)
				_, err := builtinrepo.New(ti.conn).InsertSettings(ctx, builtinrepo.InsertSettingsParams{ProjectID: uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}, BuiltinKey: "slack", UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true}, UserSessionIssuerBound: true})
				require.NoError(t, err)
				switch scenario {
				case "deleted":
					err = testrepo.New(ti.conn).ForceHardDeleteUserSessionIssuerFixture(ctx, issuer.ID)
				case "soft-deleted":
					err = testrepo.New(ti.conn).ForceSoftDeleteUserSessionIssuer(ctx, testrepo.ForceSoftDeleteUserSessionIssuerParams{ID: issuer.ID, ProjectID: *ac.ProjectID})
				case "wrong-project":
					other, createErr := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Other binding project", Slug: "other-" + uuid.NewString()[:8], OrganizationID: ac.ActiveOrganizationID})
					require.NoError(t, createErr)
					err = testrepo.New(ti.conn).ForceUserSessionIssuerOwnershipFixture(ctx, testrepo.ForceUserSessionIssuerOwnershipFixtureParams{ID: issuer.ID, ProjectID: uuid.NullUUID{UUID: other.ID, Valid: true}, OrganizationID: conv.ToPGText(ac.ActiveOrganizationID)})
				case "wrong-organization":
					otherOrg := "org_" + uuid.NewString()
					require.NoError(t, testrepo.New(ti.conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{ID: otherOrg, Name: "Other organization", Slug: "other-" + uuid.NewString(), GramAccountType: "enterprise", FreeTrialStartedAt: conv.ToPGTimestamptz(time.Now()), FreeTrialEndsAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour))}))
					err = testrepo.New(ti.conn).ForceUserSessionIssuerOwnershipFixture(ctx, testrepo.ForceUserSessionIssuerOwnershipFixtureParams{ID: issuer.ID, ProjectID: uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}, OrganizationID: conv.ToPGText(otherOrg)})
				}
				require.NoError(t, err)
				builtinIssuerFixture(t, ctx, ti, false)
				builtinIssuerFixture(t, ctx, ti, true)
			}
			_, err = ti.service.LoadResolvedMcpEndpointBySlug(ctx, testenv.NewLogger(t), builtinSlug(t, ctx, ti), mcproute.Builtin)
			require.Error(t, err, "OAuth must not select a fallback authority")
			response, err := serveBuiltinHTTP(t, ti, *ac.ProjectID, key, makeInitializeBody(), nil)
			require.NoError(t, err, "OAuth configuration cannot disable agent keys")
			require.Contains(t, response.Body.String(), "Speakeasy Built-in MCP")
			_, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, wrongKey, makeInitializeBody(), nil)
			require.Error(t, err, "issuer failure cannot bypass the explicit built-in resource grant")
			_, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, principalTokens[0], makeInitializeBody(), nil)
			require.NoError(t, err, "OAuth configuration cannot disable principal credentials")
			_, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, principalTokens[1], makeInitializeBody(), nil)
			require.Error(t, err, "principal credentials still require an explicit built-in grant")
			response, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, "", makeInitializeBody(), nil)
			require.Error(t, err)
			require.Empty(t, response.Header().Get("WWW-Authenticate"), "unavailable OAuth must not be advertised")
		})
	}
}

func TestBuiltinBinding_OrganizationChoiceRemainsStable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	issuer := builtinIssuerFixture(t, ctx, ti, true)
	slug := builtinSlug(t, ctx, ti)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, testenv.NewLogger(t), slug, mcproute.Builtin)
	require.NoError(t, err)
	require.Equal(t, issuer.ID, endpoint.UserSessionIssuerID)
	builtinIssuerFixture(t, ctx, ti, false)
	builtinIssuerFixture(t, ctx, ti, false)
	builtinIssuerFixture(t, ctx, ti, true)
	endpoint, err = ti.service.LoadResolvedMcpEndpointBySlug(ctx, testenv.NewLogger(t), slug, mcproute.Builtin)
	require.NoError(t, err)
	require.Equal(t, issuer.ID, endpoint.UserSessionIssuerID)
	settings, err := builtinrepo.New(ti.conn).GetSettings(ctx, builtinrepo.GetSettingsParams{ProjectID: uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}, BuiltinKey: "slack"})
	require.NoError(t, err)
	require.EqualValues(t, 1, settings.Version, "subsequent resolution does not rewrite the binding")
}

func TestBuiltinBinding_ConcurrentFirstSelection(t *testing.T) {
	t.Parallel()
	for _, existing := range []bool{false, true} {
		name := "insert"
		if existing {
			name = "update"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			ac, _ := contextvalues.GetAuthContext(ctx)
			issuer := builtinIssuerFixture(t, ctx, ti, false)
			if existing {
				_, err := builtinrepo.New(ti.conn).InsertSettings(ctx, builtinrepo.InsertSettingsParams{ProjectID: uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}, BuiltinKey: "slack"})
				require.NoError(t, err)
			}
			slug := builtinSlug(t, ctx, ti)
			const callers = 8
			start := make(chan struct{})
			errs := make([]error, callers)
			ids := make([]uuid.UUID, callers)
			var wg sync.WaitGroup
			for i := range callers {
				wg.Go(func() {
					<-start
					endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, testenv.NewLogger(t), slug, mcproute.Builtin)
					errs[i] = err
					if err == nil {
						ids[i] = endpoint.UserSessionIssuerID
					}
				})
			}
			close(start)
			wg.Wait()
			for i := range callers {
				require.NoError(t, errs[i])
				require.Equal(t, issuer.ID, ids[i])
			}
			settings, err := builtinrepo.New(ti.conn).GetSettings(ctx, builtinrepo.GetSettingsParams{ProjectID: uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}, BuiltinKey: "slack"})
			require.NoError(t, err)
			require.Equal(t, uuid.NullUUID{UUID: issuer.ID, Valid: true}, settings.UserSessionIssuerID)
			require.True(t, settings.UserSessionIssuerBound)
			expectedVersion := int64(1)
			if existing {
				expectedVersion = 2
			}
			require.Equal(t, expectedVersion, settings.Version)
			audits, err := testrepo.New(ti.conn).CountBuiltinIssuerBindingAuditsFixture(ctx, testrepo.CountBuiltinIssuerBindingAuditsFixtureParams{ProjectID: uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}, SubjectID: builtinmcp.ID(*ac.ProjectID, "slack").String()})
			require.NoError(t, err)
			require.EqualValues(t, 1, audits, "only the successful selector writes an audit")
		})
	}
}

func TestBuiltinBinding_CompareAndSetPreservesConcurrentAdminChoice(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	automatic := builtinIssuerFixture(t, ctx, ti, true)
	explicit := builtinIssuerFixture(t, ctx, ti, false)
	_, err := builtinrepo.New(ti.conn).InsertSettings(ctx, builtinrepo.InsertSettingsParams{ProjectID: uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}, BuiltinKey: "slack"})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, err = builtinrepo.New(tx).UpdateSettings(ctx, builtinrepo.UpdateSettingsParams{ProjectID: uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}, BuiltinKey: "slack", UserSessionIssuerID: uuid.NullUUID{UUID: explicit.ID, Valid: true}, UserSessionIssuerBound: true, ExpectedVersion: 1})
	require.NoError(t, err)
	// A stale automatic candidate must lose even when the explicit write is not yet committed.
	raceCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, bindErr := builtinrepo.New(ti.conn).BindUserSessionIssuer(raceCtx, builtinrepo.BindUserSessionIssuerParams{ProjectID: uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}, BuiltinKey: "slack", UserSessionIssuerID: uuid.NullUUID{UUID: automatic.ID, Valid: true}})
		done <- bindErr
	}()
	testenv.WaitForBackendsBlockedBy(t, raceCtx, ti.conn, testenv.BackendPID(tx), 1)
	require.NoError(t, tx.Commit(ctx))
	require.ErrorIs(t, <-done, pgx.ErrNoRows)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, testenv.NewLogger(t), builtinSlug(t, ctx, ti), mcproute.Builtin)
	require.NoError(t, err)
	require.Equal(t, explicit.ID, endpoint.UserSessionIssuerID)
	settings, err := builtinrepo.New(ti.conn).GetSettings(ctx, builtinrepo.GetSettingsParams{ProjectID: uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}, BuiltinKey: "slack"})
	require.NoError(t, err)
	require.EqualValues(t, 2, settings.Version)
}
