package authz

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestPrincipalCredentialAdmissionRejectsEachParentGate(t *testing.T) {
	t.Parallel()

	mutations := map[string]func(t *testing.T, fixture credentialAdmissionFixture){
		"suspended agent": func(t *testing.T, fixture credentialAdmissionFixture) {
			t.Helper()
			_, err := agentsrepo.New(fixture.db).SuspendAgent(t.Context(), agentsrepo.SuspendAgentParams{OrganizationID: fixture.organizationID, ID: fixture.agentID})
			require.NoError(t, err)
		},
		"revoked agent": func(t *testing.T, fixture credentialAdmissionFixture) {
			t.Helper()
			_, err := agentsrepo.New(fixture.db).RevokeAgent(t.Context(), agentsrepo.RevokeAgentParams{OrganizationID: fixture.organizationID, ID: fixture.agentID})
			require.NoError(t, err)
		},
		"deleted agent": func(t *testing.T, fixture credentialAdmissionFixture) {
			t.Helper()
			_, err := agentsrepo.New(fixture.db).DeleteAgent(t.Context(), agentsrepo.DeleteAgentParams{OrganizationID: fixture.organizationID, ID: fixture.agentID})
			require.NoError(t, err)
		},
		"owner reassignment required": func(t *testing.T, fixture credentialAdmissionFixture) {
			t.Helper()
			_, err := agentsrepo.New(fixture.db).LatchAgentsForOwnerLossByMembership(t.Context(), agentsrepo.LatchAgentsForOwnerLossByMembershipParams{
				OwnerReassignmentReason: pgtype.Text{String: "membership_loss", Valid: true},
				OrganizationID:          fixture.organizationID,
				OwnerUserID:             fixture.ownerUserID,
			})
			require.NoError(t, err)
		},
		"owner ineligible": func(t *testing.T, fixture credentialAdmissionFixture) {
			t.Helper()
			err := orgrepo.New(fixture.db).DeleteOrganizationUserRelationship(t.Context(), orgrepo.DeleteOrganizationUserRelationshipParams{
				OrganizationID: fixture.organizationID,
				UserID:         conv.ToPGText(fixture.ownerUserID),
			})
			require.NoError(t, err)
		},
	}

	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newCredentialAdmissionFixture(t)
			mutate(t, fixture)
			_, err := fixture.engine.PrepareContext(fixture.requestContext)
			requireUnauthorized(t, err)
		})
	}
}

func TestPrincipalAPIKeyAdmissionRevalidatesCredentialActivity(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(t *testing.T, fixture credentialAdmissionFixture, keyID uuid.UUID){
		"deleted": func(t *testing.T, fixture credentialAdmissionFixture, keyID uuid.UUID) {
			t.Helper()
			_, err := keysrepo.New(fixture.db).DeleteAPIKey(t.Context(), keysrepo.DeleteAPIKeyParams{ID: keyID, OrganizationID: fixture.organizationID})
			require.NoError(t, err)
		},
		"expired": func(t *testing.T, fixture credentialAdmissionFixture, keyID uuid.UUID) {
			t.Helper()
			//nolint:glint // notestingrawsql: simulate expiry after the credential profile was loaded
			_, err := fixture.db.Exec(t.Context(), `UPDATE api_keys SET expires_at = statement_timestamp() - INTERVAL '1 second' WHERE id = $1 AND organization_id = $2`, keyID, fixture.organizationID)
			require.NoError(t, err)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newCredentialAdmissionFixture(t)
			authCtx, ok := contextvalues.GetAuthContext(fixture.requestContext)
			require.True(t, ok)
			actor, ok := contextvalues.AuthenticatedActor(fixture.requestContext)
			require.True(t, ok)
			credential, ok := contextvalues.PrincipalCredentialAuthorization(fixture.requestContext)
			require.True(t, ok)

			keyID := uuid.New()
			keyHash := uuid.NewString()
			created, err := keysrepo.New(fixture.db).CreateAPIKey(t.Context(), keysrepo.CreateAPIKeyParams{
				OrganizationID: fixture.organizationID, CreatedByUserID: fixture.authorizerUserID,
				Name: "admission-" + keyID.String(), KeyPrefix: "gram_test", KeyHash: keyHash, Scopes: []string{"producer"},
			})
			require.NoError(t, err)
			//nolint:glint // notestingrawsql: AIM-194 owns the principal-key writer; this seeds its immutable profile
			_, err = fixture.db.Exec(t.Context(), `UPDATE api_keys SET scopes = '{}', subject_urn = $1, delegated_grants = $2, delegated_grants_version = $3, expires_at = statement_timestamp() + INTERVAL '1 day' WHERE id = $4`,
				actor.String(), credential.DelegatedGrants, credential.DelegatedGrantsVersion, created.ID)
			require.NoError(t, err)

			keyAuth := *authCtx
			keyAuth.APIKeyID = created.ID.String()
			requestContext := contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), &keyAuth, actor, credential)
			_, err = fixture.engine.PrepareContext(requestContext)
			require.NoError(t, err)

			mutate(t, fixture, created.ID)
			_, err = fixture.engine.PrepareContext(requestContext)
			requireUnauthorized(t, err)
		})
	}
}

func TestPrincipalCredentialAdmissionIsTenantBound(t *testing.T) {
	t.Parallel()

	fixture := newCredentialAdmissionFixture(t)
	otherOrganizationID := "org-admission-other-" + uuid.NewString()
	seedOrganization(t, t.Context(), fixture.db, otherOrganizationID)
	authCtx, ok := contextvalues.GetAuthContext(fixture.requestContext)
	require.True(t, ok)
	crossTenant := *authCtx
	crossTenant.ActiveOrganizationID = otherOrganizationID
	credential, ok := contextvalues.PrincipalCredentialAuthorization(fixture.requestContext)
	require.True(t, ok)
	actor, ok := contextvalues.AuthenticatedActor(fixture.requestContext)
	require.True(t, ok)
	ctx := contextvalues.WithPrincipalCredentialAuthorization(t.Context(), &crossTenant, actor, credential)

	_, err := fixture.engine.PrepareContext(ctx)
	requireUnauthorized(t, err)
}

func TestPrincipalCredentialAdmissionReloadsLivePolicies(t *testing.T) {
	t.Parallel()

	fixture := newCredentialAdmissionFixture(t)
	check := Check{Scope: ScopeProjectRead, ResourceID: fixture.projectID}

	prepared, err := fixture.engine.PrepareContext(fixture.requestContext)
	require.NoError(t, err)
	require.NoError(t, fixture.engine.Require(prepared, check))
	actor, ok := contextvalues.AuthenticatedActor(prepared)
	require.True(t, ok)
	require.Equal(t, "agent:"+fixture.agentID.String(), actor.String())
	authorizer, owner, ok := contextvalues.PrincipalCredentialProvenance(prepared)
	require.True(t, ok)
	require.Equal(t, fixture.authorizerUserID, authorizer)
	require.Equal(t, fixture.ownerUserID, owner)

	for name, principalURN := range map[string]func(credentialAdmissionFixture) string{
		"agent A": func(f credentialAdmissionFixture) string { return "agent:" + f.agentID.String() },
		"owner O": func(f credentialAdmissionFixture) string { return "user:" + f.ownerUserID },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newCredentialAdmissionFixture(t)
			check := Check{Scope: ScopeProjectRead, ResourceID: fixture.projectID}
			principal := principalURN(fixture)
			//nolint:glint // notestingrawsql: simulate an authoritative live-policy removal and restoration
			_, err := fixture.db.Exec(t.Context(), `DELETE FROM principal_grants WHERE organization_id = $1 AND principal_urn = $2 AND scope = $3`, fixture.organizationID, principal, string(ScopeProjectRead))
			require.NoError(t, err)

			prepared, err := fixture.engine.PrepareContext(fixture.requestContext)
			require.NoError(t, err)
			var denied *oops.ShareableError
			require.ErrorAs(t, fixture.engine.Require(prepared, check), &denied)
			require.Equal(t, oops.CodeForbidden, denied.Code)

			parsed, err := urn.ParsePrincipal(principal)
			require.NoError(t, err)
			seedGrant(t, t.Context(), fixture.db, fixture.organizationID, parsed, ScopeProjectRead, fixture.projectID)
			prepared, err = fixture.engine.PrepareContext(fixture.requestContext)
			require.NoError(t, err)
			require.NoError(t, fixture.engine.Require(prepared, check), "restoration may reactivate authority still present in immutable R")
		})
	}
}

func TestPrincipalCredentialAdmissionUsesOnlyCurrentOwnerAfterTransfer(t *testing.T) {
	t.Parallel()

	fixture := newCredentialAdmissionFixture(t)
	newOwnerUserID := "new-owner-" + uuid.NewString()
	_, err := usersrepo.New(fixture.db).UpsertUser(t.Context(), usersrepo.UpsertUserParams{
		ID: newOwnerUserID, Email: newOwnerUserID + "@example.com", DisplayName: newOwnerUserID, PhotoUrl: conv.PtrToPGText(nil), Admin: false,
	})
	require.NoError(t, err)
	_, err = orgrepo.New(fixture.db).UpsertOrganizationUserRelationship(t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: fixture.organizationID, UserID: conv.ToPGText(newOwnerUserID),
	})
	require.NoError(t, err)
	_, err = agentsrepo.New(fixture.db).TransferAgent(t.Context(), agentsrepo.TransferAgentParams{
		OwnerUserID: newOwnerUserID, OrganizationID: fixture.organizationID, ID: fixture.agentID,
	})
	require.NoError(t, err)

	check := Check{Scope: ScopeProjectRead, ResourceID: fixture.projectID}
	prepared, err := fixture.engine.PrepareContext(fixture.requestContext)
	require.NoError(t, err)
	var denied *oops.ShareableError
	require.ErrorAs(t, fixture.engine.Require(prepared, check), &denied, "former owner's grants cannot authorize after transfer")
	require.Equal(t, oops.CodeForbidden, denied.Code)

	seedGrant(t, t.Context(), fixture.db, fixture.organizationID, urn.NewPrincipal(urn.PrincipalTypeUser, newOwnerUserID), ScopeProjectRead, fixture.projectID)
	prepared, err = fixture.engine.PrepareContext(fixture.requestContext)
	require.NoError(t, err)
	require.NoError(t, fixture.engine.Require(prepared, check))
	_, owner, ok := contextvalues.PrincipalCredentialProvenance(prepared)
	require.True(t, ok)
	require.Equal(t, newOwnerUserID, owner)
}

func TestPrincipalCredentialAdmissionDeniesEveryCheckAfterRevocationCommit(t *testing.T) {
	t.Parallel()

	fixture := newCredentialAdmissionFixture(t)
	start := make(chan struct{})
	results := make(chan error, 32)
	var workers sync.WaitGroup
	for range 32 {
		workers.Go(func() {
			<-start
			_, err := fixture.engine.PrepareContext(fixture.requestContext)
			results <- err
		})
	}

	_, err := agentsrepo.New(fixture.db).RevokeAgent(t.Context(), agentsrepo.RevokeAgentParams{
		OrganizationID: fixture.organizationID,
		ID:             fixture.agentID,
	})
	require.NoError(t, err)
	close(start)
	workers.Wait()
	close(results)
	for err := range results {
		requireUnauthorized(t, err)
	}
}

type credentialAdmissionFixture struct {
	db               *pgxpool.Pool
	engine           *Engine
	requestContext   context.Context //nolint:containedctx // immutable request authentication fixture
	organizationID   string
	ownerUserID      string
	authorizerUserID string
	agentID          uuid.UUID
	projectID        string
}

func newCredentialAdmissionFixture(t *testing.T) credentialAdmissionFixture {
	t.Helper()
	ctx := t.Context()
	db := newTestDB(t)
	organizationID := "org-admission-" + uuid.NewString()
	ownerUserID := "owner-" + uuid.NewString()
	authorizerUserID := "authorizer-" + uuid.NewString()
	projectID := "project-" + uuid.NewString()
	seedOrganization(t, ctx, db, organizationID)

	for _, userID := range []string{ownerUserID, authorizerUserID} {
		_, err := usersrepo.New(db).UpsertUser(ctx, usersrepo.UpsertUserParams{
			ID: userID, Email: userID + "@example.com", DisplayName: userID, PhotoUrl: conv.PtrToPGText(nil), Admin: false,
		})
		require.NoError(t, err)
		_, err = orgrepo.New(db).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
			OrganizationID: organizationID, UserID: conv.ToPGText(userID),
		})
		require.NoError(t, err)
	}

	agent, err := agentsrepo.New(db).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: organizationID, OwnerUserID: ownerUserID, Name: "Credential admission agent",
	})
	require.NoError(t, err)
	agentPrincipal := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	ownerPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, ownerUserID)
	seedGrant(t, ctx, db, organizationID, agentPrincipal, ScopeProjectRead, projectID)
	seedGrant(t, ctx, db, organizationID, ownerPrincipal, ScopeProjectRead, projectID)

	policy, err := NewDelegatedPolicyV1([]Grant{NewGrant(ScopeProjectRead, projectID)})
	require.NoError(t, err)
	rawPolicy, err := EncodeDelegatedPolicy(CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)
	requestContext := contextvalues.WithPrincipalCredentialAuthorization(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID: organizationID,
	}, agentPrincipal, contextvalues.PrincipalCredential{
		AuthorizerUserID:       authorizerUserID,
		DelegatedGrants:        rawPolicy,
		DelegatedGrantsVersion: int32(CurrentDelegatedPolicyVersion),
	})

	return credentialAdmissionFixture{
		db: db, engine: NewEngine(testenv.NewLogger(t), db, staticChallengeLogging(false), workos.NewStubClient()), requestContext: requestContext,
		organizationID: organizationID, ownerUserID: ownerUserID, authorizerUserID: authorizerUserID, agentID: agent.ID, projectID: projectID,
	}
}

func requireUnauthorized(t *testing.T, err error) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}
