package runtimepolicy

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

// newRuntimeBoundaryFixture exercises identical authority through a real API key
// profile or an assigned workload, without changing their authentication paths.
func newRuntimeBoundaryFixture(t *testing.T, mode string) credentialAdmissionFixture {
	t.Helper()
	f := newCredentialAdmissionFixture(t)
	f.engine = authz.NewEngine(testenv.NewLogger(t), f.db, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), authz.EngineOpts{
		AdmitPrincipalCredential: AdmitPrincipalCredential,
		AdmitWorkloadSession:     AdmitWorkloadSession,
	})
	authCtx, ok := contextvalues.GetAuthContext(f.requestContext)
	require.True(t, ok)
	credential, ok := contextvalues.PrincipalCredentialAuthorization(f.requestContext)
	require.True(t, ok)
	if mode == "api-key" {
		actor := urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String())
		key, err := keysrepo.New(f.db).CreateAgentAPIKey(t.Context(), keysrepo.CreateAgentAPIKeyParams{
			OrganizationID: f.organizationID, CreatedByUserID: f.authorizerUserID,
			Name: "runtime-boundary", KeyPrefix: "gram_test", KeyHash: uuid.NewString(),
			SubjectUrn: conv.ToPGText(actor.String()), DelegatedGrants: credential.DelegatedGrants,
			DelegatedGrantsVersion: pgtype.Int4{Int32: credential.DelegatedGrantsVersion, Valid: true},
			ExpiresAt:              pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		})
		require.NoError(t, err)
		keyAuth := *authCtx
		keyAuth.APIKeyID = key.ID.String()
		f.requestContext = contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), &keyAuth, actor, credential)
		return f
	}

	issuer, err := workloadrepo.New(f.db).CreateWorkloadIssuer(t.Context(), workloadrepo.CreateWorkloadIssuerParams{
		OrganizationID: f.organizationID, Name: "Runtime boundary issuer", Tags: []string{},
		Issuer: "https://issuer.example.invalid", JwksUri: "https://issuer.example.invalid/jwks",
	})
	require.NoError(t, err)
	_, err = workloadrepo.New(f.db).UpsertWorkloadAgentAssignment(t.Context(), workloadrepo.UpsertWorkloadAgentAssignmentParams{
		OrganizationID: f.organizationID, WorkloadIssuerID: issuer.ID, Subject: "runtime-boundary",
		MatchKind: string(workloadidentity.MatchKindExact), AgentID: f.agentID,
	})
	require.NoError(t, err)
	credential.AuthorizerUserID = ""
	f.requestContext = contextvalues.WithPrincipalCredentialAuthorization(t.Context(), authCtx, urn.NewWorkloadPrincipal(issuer.ID, "runtime-boundary"), credential)
	return f
}

func requireRuntimeForbidden(t *testing.T, engine *authz.Engine, ctx context.Context, check authz.Check) {
	t.Helper()
	var denied *oops.ShareableError
	require.ErrorAs(t, engine.Require(ctx, check), &denied)
	require.Equal(t, oops.CodeForbidden, denied.Code)
}

func TestRuntimeOwnerPermissionRemovalAndRestoration(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"workload", "api-key"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newRuntimeBoundaryFixture(t, mode)
			check := authz.Check{Scope: authz.ScopeProjectRead, ResourceID: f.projectID}
			prepared, err := f.engine.PrepareContext(f.requestContext)
			require.NoError(t, err)
			require.NoError(t, f.engine.Require(prepared, check))
			owner := urn.NewPrincipal(urn.PrincipalTypeUser, f.ownerUserID)
			selectors, err := authz.NewSelector(check.Scope, check.ResourceID).MarshalJSON()
			require.NoError(t, err)
			removed, err := accessrepo.New(f.db).DeletePrincipalGrantByIdentity(t.Context(), accessrepo.DeletePrincipalGrantByIdentityParams{
				OrganizationID: f.organizationID, PrincipalUrn: owner, Scope: string(check.Scope), Selectors: selectors,
			})
			require.NoError(t, err)
			require.Equal(t, int64(1), removed)
			// Reusing the prepared context must still reload authoritative policy.
			prepared, err = f.engine.PrepareContext(prepared)
			require.NoError(t, err)
			requireRuntimeForbidden(t, f.engine, prepared, check)

			seedGrant(t, t.Context(), f.db, f.organizationID, owner, check.Scope, "*")
			seedGrant(t, t.Context(), f.db, f.organizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), check.Scope, "*")
			prepared, err = f.engine.PrepareContext(prepared)
			require.NoError(t, err)
			require.NoError(t, f.engine.Require(prepared, check))
			requireRuntimeForbidden(t, f.engine, prepared, authz.Check{Scope: check.Scope, ResourceID: "outside-ceiling"})
		})
	}
}

func TestRuntimeCurrentOwnerAfterTransfer(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"workload", "api-key"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newRuntimeBoundaryFixture(t, mode)
			check := authz.Check{Scope: authz.ScopeProjectRead, ResourceID: f.projectID}
			prepared, err := f.engine.PrepareContext(f.requestContext)
			require.NoError(t, err)
			require.NoError(t, f.engine.Require(prepared, check))
			_, err = agentsrepo.New(f.db).TransferAgent(t.Context(), agentsrepo.TransferAgentParams{
				OrganizationID: f.organizationID, ID: f.agentID, OwnerUserID: f.authorizerUserID,
			})
			require.NoError(t, err)
			prepared, err = f.engine.PrepareContext(prepared)
			require.NoError(t, err)
			requireRuntimeForbidden(t, f.engine, prepared, check)
			_, owner, ok := contextvalues.PrincipalCredentialProvenance(prepared)
			require.True(t, ok)
			require.Equal(t, f.authorizerUserID, owner)
			seedGrant(t, t.Context(), f.db, f.organizationID, urn.NewPrincipal(urn.PrincipalTypeUser, f.authorizerUserID), check.Scope, check.ResourceID)
			prepared, err = f.engine.PrepareContext(prepared)
			require.NoError(t, err)
			require.NoError(t, f.engine.Require(prepared, check))
		})
	}
}

func TestWorkloadOwnerRestorationCannotBypassAgentPolicy(t *testing.T) {
	t.Parallel()
	f := newRuntimeBoundaryFixture(t, "workload")
	owner := urn.NewPrincipal(urn.PrincipalTypeUser, f.ownerUserID)
	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String())
	check := authz.Check{Scope: authz.ScopeProjectRead, ResourceID: f.projectID}
	selectors, err := authz.NewSelector(check.Scope, check.ResourceID).MarshalJSON()
	require.NoError(t, err)
	for _, principal := range []urn.Principal{owner, agent} {
		removed, err := accessrepo.New(f.db).DeletePrincipalGrantByIdentity(t.Context(), accessrepo.DeletePrincipalGrantByIdentityParams{
			OrganizationID: f.organizationID, PrincipalUrn: principal, Scope: string(check.Scope), Selectors: selectors,
		})
		require.NoError(t, err)
		require.Equal(t, int64(1), removed)
	}
	seedGrant(t, t.Context(), f.db, f.organizationID, owner, check.Scope, check.ResourceID)
	prepared, err := f.engine.PrepareContext(f.requestContext)
	require.NoError(t, err)
	requireRuntimeForbidden(t, f.engine, prepared, check)
}

func TestWorkloadAdmissionRejectsInvalidParents(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"suspended agent", "revoked agent", "deleted agent", "missing owner", "inactive owner", "owner reassignment required", "foreign organization", "missing assignment"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := newRuntimeBoundaryFixture(t, "workload")
			switch scenario {
			case "suspended agent":
				_, err := agentsrepo.New(f.db).SuspendAgent(t.Context(), agentsrepo.SuspendAgentParams{OrganizationID: f.organizationID, ID: f.agentID})
				require.NoError(t, err)
			case "revoked agent":
				_, err := agentsrepo.New(f.db).RevokeAgent(t.Context(), agentsrepo.RevokeAgentParams{OrganizationID: f.organizationID, ID: f.agentID})
				require.NoError(t, err)
			case "deleted agent":
				_, err := agentsrepo.New(f.db).DeleteAgent(t.Context(), agentsrepo.DeleteAgentParams{OrganizationID: f.organizationID, ID: f.agentID})
				require.NoError(t, err)
			case "missing owner":
				err := orgrepo.New(f.db).DeleteOrganizationUserRelationship(t.Context(), orgrepo.DeleteOrganizationUserRelationshipParams{OrganizationID: f.organizationID, UserID: conv.ToPGText(f.ownerUserID)})
				require.NoError(t, err)
			case "inactive owner":
				err := testrepo.New(f.db).ForceSoftDeleteUser(t.Context(), f.ownerUserID)
				require.NoError(t, err)
			case "owner reassignment required":
				_, err := agentsrepo.New(f.db).LatchAgentsForOwnerLossByMembership(t.Context(), agentsrepo.LatchAgentsForOwnerLossByMembershipParams{OrganizationID: f.organizationID, OwnerUserID: f.ownerUserID, OwnerReassignmentReason: conv.ToPGText("membership_loss")})
				require.NoError(t, err)
			case "foreign organization":
				authCtx, ok := contextvalues.GetAuthContext(f.requestContext)
				require.True(t, ok)
				foreign := *authCtx
				foreign.ActiveOrganizationID = "org-foreign"
				credential, ok := contextvalues.PrincipalCredentialAuthorization(f.requestContext)
				require.True(t, ok)
				actor, ok := contextvalues.AuthenticatedActor(f.requestContext)
				require.True(t, ok)
				f.requestContext = contextvalues.WithPrincipalCredentialAuthorization(t.Context(), &foreign, actor, credential)
			case "missing assignment":
				authCtx, ok := contextvalues.GetAuthContext(f.requestContext)
				require.True(t, ok)
				credential, ok := contextvalues.PrincipalCredentialAuthorization(f.requestContext)
				require.True(t, ok)
				f.requestContext = contextvalues.WithPrincipalCredentialAuthorization(t.Context(), authCtx, urn.NewWorkloadPrincipal(uuid.New(), "unassigned"), credential)
			}
			_, err := f.engine.PrepareContext(f.requestContext)
			requireUnauthorized(t, err)
		})
	}
}

func TestWorkloadOwnerEffectiveExclusionsAndDirectPrecedence(t *testing.T) {
	t.Parallel()
	f := newRuntimeBoundaryFixture(t, "workload")
	owner := urn.NewPrincipal(urn.PrincipalTypeUser, f.ownerUserID)
	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String())
	seedGrant(t, t.Context(), f.db, f.organizationID, agent, authz.ScopeMCPWrite, "*")
	seedGrant(t, t.Context(), f.db, f.organizationID, urn.NewPrincipal(urn.PrincipalTypeUser, urn.AllUsersPrincipalID), authz.ScopeMCPWrite, "*")
	seedGrant(t, t.Context(), f.db, f.organizationID, urn.NewPrincipal(urn.PrincipalTypeUser, urn.AllUsersPrincipalID), authz.ScopeMCPBlockedConnect, "selected-server")
	policy, err := NewDelegatedPolicyV1([]authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, "*")})
	require.NoError(t, err)
	raw, err := EncodeDelegatedPolicy(CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(f.requestContext)
	require.True(t, ok)
	actor, ok := contextvalues.AuthenticatedActor(f.requestContext)
	require.True(t, ok)
	ctx := contextvalues.WithPrincipalCredentialAuthorization(t.Context(), authCtx, actor, contextvalues.PrincipalCredential{
		DelegatedGrants: raw, DelegatedGrantsVersion: int32(CurrentDelegatedPolicyVersion),
	})
	selected := authz.Check{Scope: authz.ScopeMCPConnect, ResourceID: "selected-server"}
	other := authz.Check{Scope: authz.ScopeMCPConnect, ResourceID: "other-server"}
	prepared, err := f.engine.PrepareContext(ctx)
	require.NoError(t, err)
	requireRuntimeForbidden(t, f.engine, prepared, selected)
	require.NoError(t, f.engine.Require(prepared, other), "owner inherits user:all allows, subject to its blocks")

	seedGrant(t, t.Context(), f.db, f.organizationID, owner, authz.ScopeMCPConnect, selected.ResourceID)
	prepared, err = f.engine.PrepareContext(prepared)
	require.NoError(t, err)
	require.NoError(t, f.engine.Require(prepared, selected), "direct concrete user grants override inherited blocks")
	seedGrant(t, t.Context(), f.db, f.organizationID, owner, authz.ScopeMCPBlockedConnect, selected.ResourceID)
	prepared, err = f.engine.PrepareContext(prepared)
	require.NoError(t, err)
	requireRuntimeForbidden(t, f.engine, prepared, selected)
	require.NoError(t, f.engine.Require(prepared, other), "the owner's own block only restricts the selected server")
}
