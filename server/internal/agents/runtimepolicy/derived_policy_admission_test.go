package runtimepolicy

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// withDerivedPolicy authenticates actor the way a signed principal credential
// does: no stored delegated policy, derived on every admission instead.
func withDerivedPolicy(t *testing.T, f credentialAdmissionFixture, actor urn.Principal, authorizerUserID string) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(f.requestContext)
	require.True(t, ok)
	return contextvalues.WithPrincipalCredentialAuthorization(t.Context(), authCtx, actor, contextvalues.PrincipalCredential{
		AuthorizerUserID: authorizerUserID, DelegatedGrants: nil, DelegatedGrantsVersion: 0, DerivesPolicyAtAdmission: true,
	})
}

func TestDerivedCredentialPolicyFollowsTheAuthorizerLive(t *testing.T) {
	t.Parallel()
	f := newCredentialAdmissionFixture(t)
	authorizer := urn.NewPrincipal(urn.PrincipalTypeUser, f.authorizerUserID)
	ctx := withDerivedPolicy(t, f, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), f.authorizerUserID)
	check := authz.Check{Scope: authz.ScopeProjectRead, ResourceID: f.projectID}

	prepared, err := f.engine.PrepareContext(ctx)
	require.NoError(t, err)
	requireRuntimeForbidden(t, f.engine, prepared, check)

	seedGrant(t, t.Context(), f.db, f.organizationID, authorizer, check.Scope, f.projectID)
	prepared, err = f.engine.PrepareContext(prepared)
	require.NoError(t, err)
	require.NoError(t, f.engine.Require(prepared, check), "a grant the authorizer gains applies to the next request")

	selectors, err := authz.NewSelector(check.Scope, check.ResourceID).MarshalJSON()
	require.NoError(t, err)
	removed, err := accessrepo.New(f.db).DeletePrincipalGrantByIdentity(t.Context(), accessrepo.DeletePrincipalGrantByIdentityParams{
		OrganizationID: f.organizationID, PrincipalUrn: authorizer, Scope: string(check.Scope), Selectors: selectors,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), removed)
	prepared, err = f.engine.PrepareContext(prepared)
	require.NoError(t, err)
	requireRuntimeForbidden(t, f.engine, prepared, check)
}

func TestDerivedCredentialPolicyStaysWithinTheAgentPolicy(t *testing.T) {
	t.Parallel()
	f := newCredentialAdmissionFixture(t)
	seedGrant(t, t.Context(), f.db, f.organizationID, urn.NewPrincipal(urn.PrincipalTypeUser, f.authorizerUserID), authz.ScopeProjectRead, "*")
	seedGrant(t, t.Context(), f.db, f.organizationID, urn.NewPrincipal(urn.PrincipalTypeUser, f.ownerUserID), authz.ScopeProjectRead, "*")
	ctx := withDerivedPolicy(t, f, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), f.authorizerUserID)

	prepared, err := f.engine.PrepareContext(ctx)
	require.NoError(t, err)
	require.NoError(t, f.engine.Require(prepared, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: f.projectID}))
	requireRuntimeForbidden(t, f.engine, prepared, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: "project-" + uuid.NewString()})
}

func TestDerivedWorkloadCeilingFollowsTheAgentLive(t *testing.T) {
	t.Parallel()
	f := newRuntimeBoundaryFixture(t, "workload")
	actor, ok := contextvalues.AuthenticatedActor(f.requestContext)
	require.True(t, ok)
	ctx := withDerivedPolicy(t, f, actor, "")
	outside := authz.Check{Scope: authz.ScopeProjectRead, ResourceID: "project-" + uuid.NewString()}

	prepared, err := f.engine.PrepareContext(ctx)
	require.NoError(t, err)
	require.NoError(t, f.engine.Require(prepared, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: f.projectID}))
	requireRuntimeForbidden(t, f.engine, prepared, outside)

	seedGrant(t, t.Context(), f.db, f.organizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), authz.ScopeProjectRead, "*")
	seedGrant(t, t.Context(), f.db, f.organizationID, urn.NewPrincipal(urn.PrincipalTypeUser, f.ownerUserID), authz.ScopeProjectRead, "*")
	prepared, err = f.engine.PrepareContext(prepared)
	require.NoError(t, err)
	require.NoError(t, f.engine.Require(prepared, outside), "the ceiling follows the agent's live policy")
}

func TestDerivedCredentialPolicyRejectsStoredGrants(t *testing.T) {
	t.Parallel()
	f := newCredentialAdmissionFixture(t)
	stored, ok := contextvalues.PrincipalCredentialAuthorization(f.requestContext)
	require.True(t, ok)
	authCtx, ok := contextvalues.GetAuthContext(f.requestContext)
	require.True(t, ok)
	stored.DerivesPolicyAtAdmission = true
	ctx := contextvalues.WithPrincipalCredentialAuthorization(t.Context(), authCtx, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), stored)

	_, err := AdmitPrincipalCredential(ctx, f.db)
	requireUnauthorized(t, err)
}

func TestPrincipalAPIKeyNeverDerivesPolicy(t *testing.T) {
	t.Parallel()
	f := newCredentialAdmissionFixture(t)
	authCtx, ok := contextvalues.GetAuthContext(f.requestContext)
	require.True(t, ok)
	keyAuth := *authCtx
	keyAuth.APIKeyID = uuid.NewString()
	ctx := contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), &keyAuth, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), contextvalues.PrincipalCredential{
		AuthorizerUserID: f.authorizerUserID, DelegatedGrants: nil, DelegatedGrantsVersion: 0, DerivesPolicyAtAdmission: true,
	})

	_, err := AdmitPrincipalCredential(ctx, f.db)
	requireUnauthorized(t, err)
}
