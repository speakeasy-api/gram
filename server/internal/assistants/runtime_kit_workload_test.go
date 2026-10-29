package assistants

import (
	"encoding/json"
	"fmt"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
	"github.com/stretchr/testify/require"
)

func TestKitWorkloadSubjectGeneration(t *testing.T) {
	t.Parallel()
	r := assistantRuntimeRecord{ID: uuid.New()}
	c := KitLocalCompositionConfig{Environment: "production", WorkloadNamespace: "assistants"}
	subject, sa, err := kitWorkloadSubject(r, c, "gateway-one")
	require.NoError(t, err)
	require.LessOrEqual(t, len(sa), 63)
	require.Regexp(t, `^kit-[a-f0-9]{32}-[a-f0-9]{16}$`, sa)
	require.Equal(t, "system:serviceaccount:assistants:"+sa, subject)
	again, _, err := kitWorkloadSubject(r, c, "gateway-one")
	require.NoError(t, err)
	require.Equal(t, subject, again)
	rotated, _, err := kitWorkloadSubject(r, c, "gateway-two")
	require.NoError(t, err)
	require.NotEqual(t, subject, rotated)
	r.ID = uuid.New()
	other, _, err := kitWorkloadSubject(r, c, "gateway-one")
	require.NoError(t, err)
	require.NotEqual(t, subject, other)
	_, _, err = kitWorkloadSubject(r, c, "")
	require.Error(t, err)
	c.Environment, c.WorkloadSubject = "local", "local-exact-subject"
	subject, sa, err = kitWorkloadSubject(r, c, "gateway-one")
	require.NoError(t, err)
	require.Equal(t, c.WorkloadSubject, subject)
	require.Empty(t, sa)
}

func TestKitWorkloadInferenceRequiresBothPolicies(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	allowed := []authz.Grant{authz.NewGrant(authz.ScopeInferenceExecute, authz.WildcardResource)}
	require.NoError(t, kitWorkloadInferenceAllowed(project, allowed, allowed))
	require.Error(t, kitWorkloadInferenceAllowed(project, nil, allowed))
	require.Error(t, kitWorkloadInferenceAllowed(project, allowed, nil))
	require.Error(t, kitWorkloadInferenceAllowed(project, allowed))
	require.Error(t, kitWorkloadInferenceAllowed(uuid.Nil, allowed, allowed))
}

func TestKitWorkloadRejectsForeignIdentityBeforeDatabase(t *testing.T) {
	t.Parallel()
	r := assistantRuntimeRecord{ID: uuid.New(), ProjectID: uuid.New(), AssistantID: uuid.New(), RuntimeVersion: 3}
	identity := assistantidentity.RuntimeIdentity{OrganizationID: "test-org", ProjectID: r.ProjectID, AssistantID: r.AssistantID, AgentID: uuid.New(), BindingID: uuid.New(), BindingGeneration: 1}
	require.True(t, kitWorkloadIdentityMatches(r, identity))
	identity.ProjectID = uuid.New()
	core := new(ServiceCore)
	_, err := core.provisionKitWorkload(t.Context(), r, KitLocalCompositionConfig{}, identity, "gateway")
	require.ErrorIs(t, err, ErrKitRuntimeBinding)
	r.BackendMetadataJSON, err = json.Marshal(map[string]any{"workload": KitWorkloadBinding{IssuerID: uuid.New(), Subject: "subject", Identity: identity}})
	require.NoError(t, err)
	require.ErrorIs(t, core.revokeKitWorkload(t.Context(), nil, r), ErrKitRuntimeBinding)
	r.BackendMetadataJSON = []byte(`{"workload":`)
	require.Error(t, core.revokeKitWorkload(t.Context(), nil, r))
	r.BackendMetadataJSON = []byte(`{}`)
	require.NoError(t, core.revokeKitWorkload(t.Context(), nil, r))
}

func TestKitWorkloadIssuerRequiresRuntimeOwnership(t *testing.T) {
	t.Parallel()
	issuerURL, jwksURL := "https://cluster.example", "https://cluster.example/openid/v1/jwks"
	issuer := workloadrepo.WorkloadIssuer{Name: kitWorkloadIssuerName(issuerURL), Tags: []string{"kit-runtime"}, Issuer: issuerURL, JwksUri: jwksURL, AllowAssertionReuse: pgtype.Bool{Bool: true, Valid: true}}
	require.True(t, kitOwnedWorkloadIssuer(issuer, issuerURL, jwksURL))
	for _, mutate := range []func(*workloadrepo.WorkloadIssuer){
		func(i *workloadrepo.WorkloadIssuer) { i.Name = "user-managed" },
		func(i *workloadrepo.WorkloadIssuer) { i.Tags = nil },
		func(i *workloadrepo.WorkloadIssuer) { i.AllowAssertionReuse = pgtype.Bool{} },
		func(i *workloadrepo.WorkloadIssuer) { i.AllowAssertionReuse.Bool = false },
		func(i *workloadrepo.WorkloadIssuer) { i.AllowWildcardAdmission = true },
		func(i *workloadrepo.WorkloadIssuer) { i.JwksUri = "https://other.example/jwks" },
	} {
		candidate := issuer
		mutate(&candidate)
		require.False(t, kitOwnedWorkloadIssuer(candidate, issuerURL, jwksURL))
	}
}

// Grant the owner authority before creation, so normal provisioning grants the
// agent its own inference policy rather than bypassing either live policy.
func kitWorkloadDBFixture(t *testing.T) (*ServiceCore, *pgxpool.Pool, assistantRuntimeRecord, assistantidentity.RuntimeIdentity, KitLocalCompositionConfig, uuid.UUID) {
	t.Helper()
	db, err := assistantsInfra.CloneTestDatabase(t, "kit_workload_trust")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "kit-workload-trust")
	seedProjectRead(t, db, "user-1", project)
	seedMCPConnect(t, db, "user-1", project)
	selector := authz.NewSelector(authz.ScopeInferenceExecute, authz.WildcardResource)
	selector[authz.SelectorKeyProjectID] = project.String()
	raw, err := selector.MarshalJSON()
	require.NoError(t, err)
	grant, err := accessrepo.New(db).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{OrganizationID: "org-test", PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"), Scope: string(authz.ScopeInferenceExecute), Selectors: raw})
	require.NoError(t, err)
	core := newProvisioningCore(t, db)
	core.serverURL, err = url.Parse("https://api.example")
	require.NoError(t, err)
	assistant, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Workload trust", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true, nil)
	require.NoError(t, err)
	_, err = core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, assistant.ID, assistant.Name)
	require.NoError(t, err)
	resolved, err := core.identities.ResolveRuntime(t.Context(), db, "org-test", project, assistant.ID)
	require.NoError(t, err)
	fixtures := testrepo.New(db)
	issuer, err := fixtures.InsertOrganizationTierUserSessionIssuerFixture(t.Context(), testrepo.InsertOrganizationTierUserSessionIssuerFixtureParams{OrganizationID: pgtype.Text{String: "org-test", Valid: true}, Slug: "kit-shared", AuthnChallengeMode: "none", SessionDuration: pgtype.Interval{Microseconds: 3600000000, Valid: true}})
	require.NoError(t, err)
	_, err = fixtures.SetUserSessionIssuerAuthorizationServerModeFixture(t.Context(), testrepo.SetUserSessionIssuerAuthorizationServerModeFixtureParams{OrganizationID: "org-test", IssuerID: issuer, AuthorizationServerMode: "shared"})
	require.NoError(t, err)
	r := assistantRuntimeRecord{ID: uuid.New(), ProjectID: project, AssistantID: assistant.ID, RuntimeVersion: 3}
	c := KitLocalCompositionConfig{Environment: "production", WorkloadNamespace: "assistants", WorkloadIssuerURL: "https://cluster.example", WorkloadJWKSURL: "https://cluster.example/openid/v1/jwks", MCPUserSessionIssuerID: issuer}
	return core, db, r, resolved, c, grant.ID
}

func kitWorkloadMetadata(t *testing.T, r assistantRuntimeRecord, binding *KitWorkloadBinding) assistantRuntimeRecord {
	t.Helper()
	var err error
	r.BackendMetadataJSON, err = json.Marshal(map[string]any{"workload": binding})
	require.NoError(t, err)
	return r
}

func TestKitWorkloadDBSharedIssuerAndExactRevoke(t *testing.T) {
	t.Parallel()
	core, db, r, identity, c, _ := kitWorkloadDBFixture(t)
	baseline, err := workloadrepo.New(db).ListWorkloadAdmissions(t.Context(), workloadrepo.ListWorkloadAdmissionsParams{OrganizationID: identity.OrganizationID, ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true}})
	require.NoError(t, err)
	require.NotEmpty(t, baseline, "canonical root admission exists before runtime provisioning")
	first, err := core.provisionKitWorkload(t.Context(), r, c, identity, "generation-one")
	require.NoError(t, err)
	require.Contains(t, first.Audience, c.MCPUserSessionIssuerID.String(), "organization-scoped shared issuer is supported")
	second, err := core.provisionKitWorkload(t.Context(), r, c, identity, "generation-two")
	require.NoError(t, err)
	require.Equal(t, first.IssuerID, second.IssuerID)
	require.NotEqual(t, first.Subject, second.Subject)
	issuers, err := workloadrepo.New(db).FindWorkloadIssuersByIssuer(t.Context(), workloadrepo.FindWorkloadIssuersByIssuerParams{OrganizationID: identity.OrganizationID, ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true}, Issuers: []string{c.WorkloadIssuerURL}})
	require.NoError(t, err)
	require.Len(t, issuers, 1)
	forged := *second
	forged.Generation = first.Generation
	require.ErrorIs(t, core.revokeKitWorkload(t.Context(), db, kitWorkloadMetadata(t, r, &forged)), ErrKitRuntimeBinding)
	require.NoError(t, core.revokeKitWorkload(t.Context(), db, kitWorkloadMetadata(t, r, first)))
	require.NoError(t, core.revokeKitWorkload(t.Context(), db, kitWorkloadMetadata(t, r, first)))
	admissions, err := workloadrepo.New(db).ListWorkloadAdmissions(t.Context(), workloadrepo.ListWorkloadAdmissionsParams{OrganizationID: identity.OrganizationID, ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true}})
	require.NoError(t, err)
	admissions, unrelated := kitWorkloadPartitionAdmissions(admissions, first.IssuerID)
	require.ElementsMatch(t, baseline, unrelated)
	require.Len(t, admissions, 1)
	require.Equal(t, second.Subject, admissions[0].Subject)
	assignments, err := workloadrepo.New(db).GetWorkloadAgentAssignmentForSubject(t.Context(), workloadrepo.GetWorkloadAgentAssignmentForSubjectParams{OrganizationID: identity.OrganizationID, WorkloadIssuerID: second.IssuerID, Subject: second.Subject, MatchKind: "exact"})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.Equal(t, identity.AgentID, assignments[0].AgentID)
}

// A runtime admitting and withdrawing its own workload on every start and
// teardown is machine access, so those entries go to the security log.
// Registering the cluster issuer happens once per project and is a
// configuration change. Both are attributed to the assistant's agent rather
// than to an anonymous system component.
func TestKitWorkloadDBAuditLogAndAttribution(t *testing.T) {
	t.Parallel()
	core, db, r, identity, c, _ := kitWorkloadDBFixture(t)
	configurationAdmits, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)
	configurationWithdrawals, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionWorkloadAdmissionWithdraw)
	require.NoError(t, err)

	binding, err := core.provisionKitWorkload(t.Context(), r, c, identity, "generation-one")
	require.NoError(t, err)

	issuerCreate, err := audittest.LatestAuditLogByAction(t.Context(), db, audit.ActionWorkloadIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, string(urn.PrincipalTypeAgent), issuerCreate.ActorType)
	require.Equal(t, identity.AgentID.String(), issuerCreate.ActorID)

	admit, err := audittest.LatestSecurityAuditLogByAction(t.Context(), db, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)
	require.Equal(t, binding.Subject, admit.SubjectDisplay)
	require.Equal(t, string(urn.PrincipalTypeAgent), admit.ActorType)
	require.Equal(t, identity.AgentID.String(), admit.ActorID)

	require.NoError(t, core.revokeKitWorkload(t.Context(), db, kitWorkloadMetadata(t, r, binding)))
	withdraw, err := audittest.LatestSecurityAuditLogByAction(t.Context(), db, audit.ActionWorkloadAdmissionWithdraw)
	require.NoError(t, err)
	require.Equal(t, binding.Subject, withdraw.SubjectDisplay)
	require.Equal(t, string(urn.PrincipalTypeAgent), withdraw.ActorType)
	require.Equal(t, identity.AgentID.String(), withdraw.ActorID)

	admits, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)
	require.Equal(t, configurationAdmits, admits, "the runtime's admit stays out of the configuration log")
	withdrawals, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionWorkloadAdmissionWithdraw)
	require.NoError(t, err)
	require.Equal(t, configurationWithdrawals, withdrawals, "the runtime's withdrawal stays out of the configuration log")
}

func TestKitWorkloadDBLocalSubjectCannotBeReused(t *testing.T) {
	t.Parallel()
	core, db, r, identity, c, _ := kitWorkloadDBFixture(t)
	c.Environment, c.WorkloadSubject = "local", "local-generation-one"
	first, err := core.provisionKitWorkload(t.Context(), r, c, identity, "generation-one")
	require.NoError(t, err)
	_, err = core.provisionKitWorkload(t.Context(), r, c, identity, "generation-two")
	require.ErrorContains(t, err, "cannot be reused")
	require.NoError(t, core.revokeKitWorkload(t.Context(), db, kitWorkloadMetadata(t, r, first)))
	_, err = core.provisionKitWorkload(t.Context(), r, c, identity, "generation-two")
	require.ErrorContains(t, err, "cannot be reused")
	_, err = workloadrepo.New(db).SoftDeleteWorkloadIssuer(t.Context(), workloadrepo.SoftDeleteWorkloadIssuerParams{OrganizationID: identity.OrganizationID, ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true}, ID: first.IssuerID})
	require.NoError(t, err)
	_, err = core.provisionKitWorkload(t.Context(), r, c, identity, "generation-two")
	require.ErrorContains(t, err, "cannot be reused", "deleted admissions under a deleted issuer still fence old assertions")
	c.WorkloadSubject = "local-generation-two"
	second, err := core.provisionKitWorkload(t.Context(), r, c, identity, "generation-two")
	require.NoError(t, err)
	require.NotEqual(t, first.IssuerID, second.IssuerID)
}

func TestKitWorkloadDBInferenceRequiresOwnerAndAgent(t *testing.T) {
	t.Parallel()
	for _, denied := range []string{"owner", "agent"} {
		t.Run(denied, func(t *testing.T) {
			t.Parallel()
			core, db, r, identity, c, ownerGrant := kitWorkloadDBFixture(t)
			if denied == "owner" {
				count, err := accessrepo.New(db).DeletePrincipalGrant(t.Context(), accessrepo.DeletePrincipalGrantParams{OrganizationID: identity.OrganizationID, ID: ownerGrant})
				require.NoError(t, err)
				require.EqualValues(t, 1, count)
			} else {
				grants, err := agentrepo.New(db).ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{OrganizationID: identity.OrganizationID, AgentID: identity.AgentID})
				require.NoError(t, err)
				removed := false
				for _, grant := range grants {
					if grant.Scope == string(authz.ScopeInferenceExecute) {
						_, err = agentrepo.New(db).DeleteAgentPolicyGrant(t.Context(), agentrepo.DeleteAgentPolicyGrantParams{OrganizationID: identity.OrganizationID, AgentID: identity.AgentID, GrantID: grant.ID})
						require.NoError(t, err)
						removed = true
					}
				}
				require.True(t, removed)
			}
			_, err := core.provisionKitWorkload(t.Context(), r, c, identity, "generation-one")
			require.ErrorContains(t, err, "both require inference:execute permission")
			issuers, err := workloadrepo.New(db).FindWorkloadIssuersByIssuer(t.Context(), workloadrepo.FindWorkloadIssuersByIssuerParams{OrganizationID: identity.OrganizationID, ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true}, Issuers: []string{c.WorkloadIssuerURL}})
			require.NoError(t, err)
			require.Empty(t, issuers)
		})
	}
}

func TestKitWorkloadDBExactAdmissionOwnershipCollision(t *testing.T) {
	t.Parallel()
	core, db, r, identity, c, _ := kitWorkloadDBFixture(t)
	baseline, err := workloadrepo.New(db).ListWorkloadAdmissions(t.Context(), workloadrepo.ListWorkloadAdmissionsParams{OrganizationID: identity.OrganizationID, ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true}})
	require.NoError(t, err)
	require.NotEmpty(t, baseline, "canonical root admission exists before runtime provisioning")
	binding, err := core.provisionKitWorkload(t.Context(), r, c, identity, "generation-one")
	require.NoError(t, err)
	q := workloadrepo.New(db)
	project := uuid.NullUUID{UUID: r.ProjectID, Valid: true}
	admissions, err := q.ListWorkloadAdmissions(t.Context(), workloadrepo.ListWorkloadAdmissionsParams{OrganizationID: identity.OrganizationID, ProjectID: project})
	require.NoError(t, err)
	admissions, unrelated := kitWorkloadPartitionAdmissions(admissions, binding.IssuerID)
	require.ElementsMatch(t, baseline, unrelated)
	require.Len(t, admissions, 1)
	_, err = q.UpdateWorkloadAdmission(t.Context(), workloadrepo.UpdateWorkloadAdmissionParams{OrganizationID: identity.OrganizationID, ProjectID: project, ID: admissions[0].ID, Name: pgtype.Text{String: "another-runtime", Valid: true}, Tags: []string{"kit-runtime"}})
	require.NoError(t, err)
	_, err = core.provisionKitWorkload(t.Context(), r, c, identity, "generation-one")
	require.ErrorContains(t, err, "belongs to another runtime")
	require.ErrorIs(t, core.revokeKitWorkload(t.Context(), db, kitWorkloadMetadata(t, r, binding)), ErrKitRuntimeBinding)
	remaining, err := q.GetWorkloadAdmission(t.Context(), workloadrepo.GetWorkloadAdmissionParams{OrganizationID: identity.OrganizationID, ProjectID: project, ID: admissions[0].ID})
	require.NoError(t, err)
	require.Equal(t, "another-runtime", remaining.Name.String)
	assignments, err := q.GetWorkloadAgentAssignmentForSubject(t.Context(), workloadrepo.GetWorkloadAgentAssignmentForSubjectParams{OrganizationID: identity.OrganizationID, WorkloadIssuerID: binding.IssuerID, Subject: binding.Subject, MatchKind: "exact"})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	after, err := q.ListWorkloadAdmissions(t.Context(), workloadrepo.ListWorkloadAdmissionsParams{OrganizationID: identity.OrganizationID, ProjectID: project})
	require.NoError(t, err)
	_, unrelated = kitWorkloadPartitionAdmissions(after, binding.IssuerID)
	require.ElementsMatch(t, baseline, unrelated)
}

func TestKitWorkloadDBSavepointRollbackLeavesNoTrust(t *testing.T) {
	t.Parallel()
	core, db, r, identity, c, _ := kitWorkloadDBFixture(t)
	baseline, err := workloadrepo.New(db).ListWorkloadAdmissions(t.Context(), workloadrepo.ListWorkloadAdmissionsParams{OrganizationID: identity.OrganizationID, ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true}})
	require.NoError(t, err)
	require.NotEmpty(t, baseline, "canonical root admission exists before runtime provisioning")
	outer := testenv.BeginTx(t, t.Context(), db)
	var binding *KitWorkloadBinding
	err = pgx.BeginFunc(t.Context(), outer, func(savepoint pgx.Tx) error {
		r.LifecycleTx = savepoint
		var provisionErr error
		binding, provisionErr = core.provisionKitWorkload(t.Context(), r, c, identity, "failed-generation")
		require.NoError(t, provisionErr)
		// Returning the post-provision composition error rolls back this
		// savepoint while leaving the outer cleanup transaction committable.
		_, compositionErr := kitCompositionMCP(c, binding)
		if compositionErr != nil {
			return fmt.Errorf("compose test workload: %w", compositionErr)
		}
		return nil
	})
	require.ErrorContains(t, err, "requires workload binding and resource")
	require.NoError(t, outer.Commit(t.Context()))
	q := workloadrepo.New(db)
	project := uuid.NullUUID{UUID: r.ProjectID, Valid: true}
	issuers, err := q.FindWorkloadIssuersByIssuer(t.Context(), workloadrepo.FindWorkloadIssuersByIssuerParams{OrganizationID: identity.OrganizationID, ProjectID: project, Issuers: []string{c.WorkloadIssuerURL}})
	require.NoError(t, err)
	require.Empty(t, issuers)
	admissions, err := q.ListWorkloadAdmissions(t.Context(), workloadrepo.ListWorkloadAdmissionsParams{OrganizationID: identity.OrganizationID, ProjectID: project})
	require.NoError(t, err)
	admissions, unrelated := kitWorkloadPartitionAdmissions(admissions, binding.IssuerID)
	require.Empty(t, admissions)
	require.ElementsMatch(t, baseline, unrelated)
	assignments, err := q.GetWorkloadAgentAssignmentForSubject(t.Context(), workloadrepo.GetWorkloadAgentAssignmentForSubjectParams{OrganizationID: identity.OrganizationID, WorkloadIssuerID: binding.IssuerID, Subject: binding.Subject, MatchKind: "exact"})
	require.NoError(t, err)
	require.Empty(t, assignments)
}

func TestKitWorkloadDBDifferentAssistantsShareIssuerNotSubjects(t *testing.T) {
	t.Parallel()
	core, db, firstRuntime, firstIdentity, config, _ := kitWorkloadDBFixture(t)
	assistant, err := core.CreateAssistant(t.Context(), firstIdentity.OrganizationID, firstRuntime.ProjectID, "user-1", "Second workload assistant", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true, nil)
	require.NoError(t, err)
	_, err = core.resolveDashboardTriggerInstance(t.Context(), firstIdentity.OrganizationID, firstRuntime.ProjectID, assistant.ID, assistant.Name)
	require.NoError(t, err)
	resolved, err := core.identities.ResolveRuntime(t.Context(), db, firstIdentity.OrganizationID, firstRuntime.ProjectID, assistant.ID)
	require.NoError(t, err)
	secondIdentity := resolved
	require.NotEqual(t, firstIdentity.AgentID, secondIdentity.AgentID)
	secondRuntime := assistantRuntimeRecord{ID: uuid.New(), ProjectID: firstRuntime.ProjectID, AssistantID: assistant.ID, RuntimeVersion: 3}

	first, err := core.provisionKitWorkload(t.Context(), firstRuntime, config, firstIdentity, "shared-generation")
	require.NoError(t, err)
	second, err := core.provisionKitWorkload(t.Context(), secondRuntime, config, secondIdentity, "shared-generation")
	require.NoError(t, err)
	require.Equal(t, first.IssuerID, second.IssuerID)
	require.NotEqual(t, first.Subject, second.Subject)
	q := workloadrepo.New(db)
	issuers, err := q.FindWorkloadIssuersByIssuer(t.Context(), workloadrepo.FindWorkloadIssuersByIssuerParams{OrganizationID: firstIdentity.OrganizationID, ProjectID: uuid.NullUUID{UUID: firstRuntime.ProjectID, Valid: true}, Issuers: []string{config.WorkloadIssuerURL}})
	require.NoError(t, err)
	require.Len(t, issuers, 1)
	for _, binding := range []*KitWorkloadBinding{first, second} {
		assignments, err := q.GetWorkloadAgentAssignmentForSubject(t.Context(), workloadrepo.GetWorkloadAgentAssignmentForSubjectParams{OrganizationID: binding.Identity.OrganizationID, WorkloadIssuerID: binding.IssuerID, Subject: binding.Subject, MatchKind: "exact"})
		require.NoError(t, err)
		require.Len(t, assignments, 1)
		require.Equal(t, binding.Identity.AgentID, assignments[0].AgentID)
	}

	config.Environment, config.WorkloadSubject = "local", "local-first-assistant-generation"
	local, err := core.provisionKitWorkload(t.Context(), firstRuntime, config, firstIdentity, "local-generation-one")
	require.NoError(t, err)
	before, err := q.GetWorkloadAgentAssignmentForSubject(t.Context(), workloadrepo.GetWorkloadAgentAssignmentForSubjectParams{OrganizationID: firstIdentity.OrganizationID, WorkloadIssuerID: local.IssuerID, Subject: local.Subject, MatchKind: "exact"})
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.Equal(t, firstIdentity.AgentID, before[0].AgentID)
	_, err = core.provisionKitWorkload(t.Context(), secondRuntime, config, secondIdentity, "local-generation-two")
	require.ErrorContains(t, err, "cannot be reused")
	after, err := q.GetWorkloadAgentAssignmentForSubject(t.Context(), workloadrepo.GetWorkloadAgentAssignmentForSubjectParams{OrganizationID: firstIdentity.OrganizationID, WorkloadIssuerID: local.IssuerID, Subject: local.Subject, MatchKind: "exact"})
	require.NoError(t, err)
	require.Equal(t, before, after, "failed provisioning must not reassign the first assistant's subject")
}

func kitWorkloadPartitionAdmissions(rows []workloadrepo.ListWorkloadAdmissionsRow, issuer uuid.UUID) (runtime, unrelated []workloadrepo.ListWorkloadAdmissionsRow) {
	for _, row := range rows {
		if row.WorkloadIssuerID == issuer {
			runtime = append(runtime, row)
		} else {
			unrelated = append(unrelated, row)
		}
	}
	return runtime, unrelated
}

func TestKitWorkloadDBConcurrentCrossProjectLocalSubject(t *testing.T) {
	t.Parallel()
	core, db, firstRuntime, firstIdentity, config, _ := kitWorkloadDBFixture(t)
	project := newProvisioningProject(t, db, "kit-workload-second-project")
	seedProjectRead(t, db, "user-1", project)
	seedMCPConnect(t, db, "user-1", project)
	selector := authz.NewSelector(authz.ScopeInferenceExecute, authz.WildcardResource)
	selector[authz.SelectorKeyProjectID] = project.String()
	raw, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(db).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{OrganizationID: firstIdentity.OrganizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"), Scope: string(authz.ScopeInferenceExecute), Selectors: raw})
	require.NoError(t, err)
	assistant, err := core.CreateAssistant(t.Context(), firstIdentity.OrganizationID, project, "user-1", "Cross-project workload assistant", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true, nil)
	require.NoError(t, err)
	_, err = core.resolveDashboardTriggerInstance(t.Context(), firstIdentity.OrganizationID, project, assistant.ID, assistant.Name)
	require.NoError(t, err)
	resolved, err := core.identities.ResolveRuntime(t.Context(), db, firstIdentity.OrganizationID, project, assistant.ID)
	require.NoError(t, err)
	secondIdentity := resolved
	require.NotEqual(t, firstIdentity.AgentID, secondIdentity.AgentID)
	secondRuntime := assistantRuntimeRecord{ID: uuid.New(), ProjectID: project, AssistantID: assistant.ID, RuntimeVersion: 3}
	config.Environment, config.WorkloadSubject = "local", "cross-project-shared-subject"
	type result struct {
		binding *KitWorkloadBinding
		err     error
	}
	start := make(chan struct{})
	ready := make(chan struct{}, 2)
	results := make(chan result, 2)
	ctx := t.Context()
	for _, candidate := range []struct {
		runtime  assistantRuntimeRecord
		identity assistantidentity.RuntimeIdentity
	}{{firstRuntime, firstIdentity}, {secondRuntime, secondIdentity}} {
		go func(r assistantRuntimeRecord, identity assistantidentity.RuntimeIdentity) {
			ready <- struct{}{}
			<-start
			binding, err := core.provisionKitWorkload(ctx, r, config, identity, "concurrent-generation")
			results <- result{binding: binding, err: err}
		}(candidate.runtime, candidate.identity)
	}
	<-ready
	<-ready
	close(start)
	var winner *KitWorkloadBinding
	successes, failures := 0, 0
	for range 2 {
		outcome := <-results
		if outcome.err == nil {
			successes++
			winner = outcome.binding
		} else {
			failures++
			require.ErrorContains(t, outcome.err, "cannot be reused")
			require.Nil(t, outcome.binding)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, failures)
	require.NotNil(t, winner)
	q := workloadrepo.New(db)
	for _, projectID := range []uuid.UUID{firstRuntime.ProjectID, secondRuntime.ProjectID} {
		issuers, err := q.FindWorkloadIssuersByIssuer(ctx, workloadrepo.FindWorkloadIssuersByIssuerParams{OrganizationID: firstIdentity.OrganizationID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Issuers: []string{config.WorkloadIssuerURL}})
		require.NoError(t, err)
		if projectID == winner.Identity.ProjectID {
			require.Len(t, issuers, 1)
			require.Equal(t, winner.IssuerID, issuers[0].ID)
		} else {
			require.Empty(t, issuers, "losing transaction must not leave an orphan issuer")
		}
	}
	assignments, err := q.GetWorkloadAgentAssignmentForSubject(ctx, workloadrepo.GetWorkloadAgentAssignmentForSubjectParams{OrganizationID: firstIdentity.OrganizationID, WorkloadIssuerID: winner.IssuerID, Subject: config.WorkloadSubject, MatchKind: "exact"})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.Equal(t, winner.Identity.AgentID, assignments[0].AgentID)
}

func TestKitWorkloadDBRetiredFenceAfterIdentityTombstone(t *testing.T) {
	t.Parallel()
	for _, deleted := range []bool{false, true} {
		t.Run(fmt.Sprintf("deleted=%t", deleted), func(t *testing.T) {
			t.Parallel()
			core, db, r, identity, c, _ := kitWorkloadDBFixture(t)
			binding, err := core.provisionKitWorkload(t.Context(), r, c, identity, "retired-generation")
			require.NoError(t, err)
			r = kitWorkloadMetadata(t, r, binding)
			q := assistantrepo.New(db)
			chat := uuid.New()
			require.NoError(t, q.UpsertAssistantChat(t.Context(), assistantrepo.UpsertAssistantChatParams{ChatID: chat, ProjectID: r.ProjectID, OrganizationID: identity.OrganizationID}))
			thread, err := q.UpsertAssistantThread(t.Context(), assistantrepo.UpsertAssistantThreadParams{AssistantID: r.AssistantID, ProjectID: r.ProjectID, CorrelationID: uuid.New().String(), ChatID: chat, SourceKind: sourceKindSlack, SourceRefJson: []byte("{}")})
			require.NoError(t, err)
			endedAt := pgtype.Timestamptz{Time: time.Now(), Valid: !deleted}
			deletedAt := pgtype.Timestamptz{Time: time.Now(), Valid: deleted}
			require.NoError(t, q.CreateAssistantRuntime(t.Context(), assistantrepo.CreateAssistantRuntimeParams{ID: r.ID, AssistantThreadID: thread, AssistantID: r.AssistantID, ProjectID: r.ProjectID, Backend: runtimeBackendGKE, BackendMetadataJson: r.BackendMetadataJSON, State: "stopped", UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, EndedAt: endedAt, DeletedAt: deletedAt}))
			require.NoError(t, q.SetKitRuntimeMetadataFixture(t.Context(), assistantrepo.SetKitRuntimeMetadataFixtureParams{ProjectID: r.ProjectID, RuntimeID: r.ID, RuntimeVersion: 3, Metadata: r.BackendMetadataJSON}))
			require.NoError(t, identityrepo.New(db).TombstoneAssistantBinding(t.Context(), identityrepo.TombstoneAssistantBindingParams{ProjectID: r.ProjectID, AssistantID: r.AssistantID}))
			require.NoError(t, q.DeleteAssistant(t.Context(), assistantrepo.DeleteAssistantParams{ProjectID: r.ProjectID, AssistantID: r.AssistantID}))
			admitted, err := workloadidentity.IsAdmitted(t.Context(), db, workloadidentity.AdmissionParams{OrganizationID: identity.OrganizationID, ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true}, WorkloadIssuerID: binding.IssuerID, Subject: binding.Subject})
			require.NoError(t, err)
			require.True(t, admitted, "retirement and identity tombstoning must not make the withdrawal regression vacuous")
			// Exercise persisted metadata fallback, without an active identity resolver.
			r.BackendMetadataJSON = nil
			require.NoError(t, core.fenceKitRuntimeAlreadyLocked(t.Context(), r))
			require.NoError(t, q.UpdateAssistantRuntimeMetadata(t.Context(), assistantrepo.UpdateAssistantRuntimeMetadataParams{ProjectID: r.ProjectID, RuntimeID: r.ID, BackendMetadataJson: []byte("{}")}))
			admitted, err = workloadidentity.IsAdmitted(t.Context(), db, workloadidentity.AdmissionParams{OrganizationID: identity.OrganizationID, ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true}, WorkloadIssuerID: binding.IssuerID, Subject: binding.Subject})
			require.NoError(t, err)
			require.False(t, admitted, "ordinary wake admission must remain denied after metadata clearing")
			_, assigned, err := workloadidentity.ResolveAssignedAgent(t.Context(), db, workloadidentity.AssignmentParams{OrganizationID: identity.OrganizationID, WorkloadIssuerID: binding.IssuerID, Subject: binding.Subject})
			require.NoError(t, err)
			require.False(t, assigned)
		})
	}
}

func TestKitRuntimeIdentityRejectsOwnerReassignmentLatch(t *testing.T) {
	t.Parallel()
	core, db, r, identity, config, _ := kitWorkloadDBFixture(t)
	require.NoError(t, core.identities.ValidateRuntime(t.Context(), db, identity))
	rows, err := agentrepo.New(db).LatchAgentsForOwnerLossByMembership(t.Context(), agentrepo.LatchAgentsForOwnerLossByMembershipParams{OrganizationID: identity.OrganizationID, OwnerUserID: "user-1", OwnerReassignmentReason: pgtype.Text{String: "membership_removed", Valid: true}})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	require.ErrorIs(t, core.identities.ValidateRuntime(t.Context(), db, identity), assistantidentity.ErrInvalidIdentity)
	_, err = core.provisionKitWorkload(t.Context(), r, config, identity, "owner-latched")
	require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
}
