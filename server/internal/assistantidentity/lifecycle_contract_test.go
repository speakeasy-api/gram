package assistantidentity_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	policyrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

func TestAgentSuspensionIsTemporaryAndPreservesBinding(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.provision(t)
	q := agentrepo.New(f.db)
	_, err := q.SuspendAgent(t.Context(), agentrepo.SuspendAgentParams{OrganizationID: f.org, ID: id.AgentID})
	require.NoError(t, err)
	require.ErrorIs(t, testIdentityService.Validate(t.Context(), f.db, id), assistantidentity.ErrInvalidIdentity)
	paused, err := testIdentityService.Resolve(t.Context(), f.db, f.org, f.project, f.assistant, f.trigger)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Unavailable, paused.State)
	_, err = q.ResumeAgent(t.Context(), agentrepo.ResumeAgentParams{OrganizationID: f.org, ID: id.AgentID})
	require.NoError(t, err)
	require.NoError(t, testIdentityService.Validate(t.Context(), f.db, id))
}

func TestAssignmentChangeInvalidatesCurrentBinding(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.provision(t)
	replacement, err := agentrepo.New(f.db).CreateAgent(t.Context(), agentrepo.CreateAgentParams{OrganizationID: f.org, ProjectID: uuid.NullUUID{UUID: f.project, Valid: true}, OwnerUserID: f.actor, Name: "Replacement"})
	require.NoError(t, err)
	for _, agent := range []uuid.UUID{replacement.ID} {
		_, err = policyrepo.New(f.db).UpsertWorkloadAgentAssignment(t.Context(), policyrepo.UpsertWorkloadAgentAssignmentParams{OrganizationID: f.org, WorkloadIssuerID: id.IssuerID, Subject: id.Subject, MatchKind: "exact", AgentID: agent})
		require.NoError(t, err)
		require.ErrorIs(t, testIdentityService.Validate(t.Context(), f.db, id), assistantidentity.ErrInvalidIdentity)
	}
	resolved, err := testIdentityService.Resolve(t.Context(), f.db, f.org, f.project, f.assistant, f.trigger)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Tombstoned, resolved.State)
}

func TestIssuerKeyMismatchInvalidatesCurrentBinding(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.provision(t)
	q := policyrepo.New(f.db)
	issuer, err := q.GetWorkloadIssuer(t.Context(), policyrepo.GetWorkloadIssuerParams{OrganizationID: f.org, ProjectID: uuid.NullUUID{UUID: f.project, Valid: true}, ID: id.IssuerID})
	require.NoError(t, err)
	require.Equal(t, "https://platform.example.invalid", issuer.Issuer)
	require.Equal(t, "https://platform.example.invalid/.well-known/jwks.json", issuer.JwksUri)
	for _, keys := range []string{"https://other.example.invalid/keys"} {
		_, err = q.UpdateWorkloadIssuer(t.Context(), policyrepo.UpdateWorkloadIssuerParams{OrganizationID: f.org, ProjectID: issuer.ProjectID, ID: issuer.ID, Name: issuer.Name, Description: issuer.Description, Tags: issuer.Tags, JwksUri: keys})
		require.NoError(t, err)
		require.ErrorIs(t, testIdentityService.Validate(t.Context(), f.db, id), assistantidentity.ErrInvalidIdentity)
	}
}

func TestOwnerChangeInvalidatesCurrentBinding(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.provision(t)
	q := repo.New(f.db)
	require.NoError(t, q.FixtureCreateUser(t.Context(), repo.FixtureCreateUserParams{ID: "replacement-owner", Email: "replacement@example.invalid"}))
	require.NoError(t, q.FixtureCreateMembership(t.Context(), repo.FixtureCreateMembershipParams{OrganizationID: f.org, UserID: conv.ToPGText("replacement-owner")}))
	for _, owner := range []string{"replacement-owner"} {
		_, err := agentrepo.New(f.db).TransferAgent(t.Context(), agentrepo.TransferAgentParams{OrganizationID: f.org, ID: id.AgentID, OwnerUserID: owner})
		require.NoError(t, err)
		require.ErrorIs(t, testIdentityService.Validate(t.Context(), f.db, id), assistantidentity.ErrInvalidIdentity)
	}
	resolved, err := testIdentityService.Resolve(t.Context(), f.db, f.org, f.project, f.assistant, f.trigger)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Tombstoned, resolved.State)
}

func TestDeploymentIssuerMismatchFailsClosed(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.provision(t)
	other, err := assistantidentity.New("https://other.example.invalid", false)
	require.NoError(t, err)
	require.ErrorIs(t, other.Validate(t.Context(), f.db, id), assistantidentity.ErrInvalidIdentity)
}

func TestSharedPlatformIssuerDoesNotShareTenantMapping(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first := f.provision(t)
	q := repo.New(f.db)
	other := fixture{db: f.db, org: "org-other-identity", project: uuid.New(), assistant: uuid.New(), trigger: uuid.New(), actor: f.actor}
	require.NoError(t, q.FixtureCreateOrganization(t.Context(), other.org))
	require.NoError(t, q.FixtureCreateMembership(t.Context(), repo.FixtureCreateMembershipParams{OrganizationID: other.org, UserID: conv.ToPGText(other.actor)}))
	require.NoError(t, q.FixtureCreateProject(t.Context(), repo.FixtureCreateProjectParams{ID: other.project, OrganizationID: other.org}))
	require.NoError(t, q.FixtureCreateAssistant(t.Context(), repo.FixtureCreateAssistantParams{ID: other.assistant, OrganizationID: other.org, ProjectID: other.project, Creator: conv.ToPGText(other.actor)}))
	require.NoError(t, q.FixtureCreateRoot(t.Context(), repo.FixtureCreateRootParams{ID: other.trigger, OrganizationID: other.org, ProjectID: other.project, DefinitionSlug: "dashboard", TargetRef: other.assistant.String()}))
	second := other.provision(t)
	require.NotEqual(t, first.IssuerID, second.IssuerID)
	require.NotEqual(t, first.AgentID, second.AgentID)
	first.OrganizationID = other.org
	require.Error(t, testIdentityService.Validate(t.Context(), f.db, first))
	second.IssuerID = first.IssuerID
	require.ErrorIs(t, testIdentityService.Validate(t.Context(), f.db, second), assistantidentity.ErrInvalidIdentity)
}

func TestConfiguredSkillIncludesBoundedProjectGate(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	q := skillsrepo.New(f.db)
	skill, err := q.CreateSkill(t.Context(), skillsrepo.CreateSkillParams{ProjectID: f.project, Name: "identity-skill", DisplayName: "Identity skill", Summary: pgtype.Text{}})
	require.NoError(t, err)
	_, err = q.CreateSkillVersion(t.Context(), skillsrepo.CreateSkillVersionParams{ProjectID: f.project, SkillID: skill.ID, Content: "Skill instructions", CanonicalSha256: "fixture-canonical", RawSha256: "fixture-raw", Description: pgtype.Text{}, Metadata: []byte("{}"), SpecValid: true, ValidationErrors: []byte("[]"), CreatedByUserID: f.actor})
	require.NoError(t, err)
	_, err = q.CreateSkillDistribution(t.Context(), skillsrepo.CreateSkillDistributionParams{ProjectID: f.project, SkillID: skill.ID, AssistantID: uuid.NullUUID{UUID: f.assistant, Valid: true}, PluginID: uuid.NullUUID{}, PinnedVersionID: uuid.NullUUID{}, Channel: "assistant", CreatedByUserID: f.actor})
	require.NoError(t, err)
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, f.actor)
	f.grant(t, actor, authz.ScopeSkillRead, f.project.String())
	f.grant(t, actor, authz.ScopeSkillRead, skill.ID.String())
	id := f.provision(t)
	ceiling, err := testIdentityService.SnapshotCeiling(t.Context(), f.db, id)
	require.NoError(t, err)
	require.Contains(t, string(ceiling.Policy), f.project.String())
	require.Contains(t, string(ceiling.Policy), skill.ID.String())
}

func TestAssignmentWithdrawalWithoutLiveAssignmentIsNoop(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.provision(t)
	q := repo.New(f.db)
	require.NoError(t, q.FixtureWithdrawAssignment(t.Context(), repo.FixtureWithdrawAssignmentParams{OrganizationID: f.org, IssuerID: id.IssuerID, Subject: id.Subject}))
	params := repo.GetTriggerBindingParams{OrganizationID: f.org, ProjectID: f.project, TriggerID: f.trigger, PlatformIssuer: "https://platform.example.invalid", PlatformJwksUri: "https://platform.example.invalid/.well-known/jwks.json"}
	before, err := q.GetTriggerBinding(t.Context(), params)
	require.NoError(t, err)
	require.False(t, before.Deleted)
	removed, err := policyrepo.New(f.db).SoftDeleteWorkloadAgentAssignmentForSubject(t.Context(), policyrepo.SoftDeleteWorkloadAgentAssignmentForSubjectParams{OrganizationID: f.org, WorkloadIssuerID: id.IssuerID, Subject: id.Subject, MatchKind: "exact"})
	require.NoError(t, err)
	require.Empty(t, removed)
	after, err := q.GetTriggerBinding(t.Context(), params)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
