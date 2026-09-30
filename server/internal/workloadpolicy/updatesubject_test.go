package workloadpolicy_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	agentsRepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// admitLabelled admits channelOne at the organization tier with a label and
// tags, so a test can tell an unchanged field from one reset to its default.
func admitLabelled(t *testing.T, ctx context.Context, ti *testInstance, agentID uuid.UUID) *types.WorkloadAdmission {
	t.Helper()

	policy, err := ti.service.AdmitSubject(ctx, &gen.AdmitSubjectPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Issuer:           anthropicIssuer,
		Subject:          channelOne,
		MatchKind:        string(workloadidentity.MatchKindExact),
		Name:             new("Deploy channel"),
		Tags:             []string{"production", "slack"},
		AgentID:          agentID.String(),
		ProjectScoped:    false,
	})
	require.NoError(t, err)

	return onlyAdmission(t, policy)
}

func updateSubjectPayload(id string) *gen.UpdateSubjectPayload {
	return &gen.UpdateSubjectPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               id,
		Name:             nil,
		Tags:             nil,
		AgentID:          nil,
	}
}

func onlyAdmission(t *testing.T, policy *gen.WorkloadIdentityPolicy) *types.WorkloadAdmission {
	t.Helper()
	require.Len(t, policy.Admissions, 1)
	return policy.Admissions[0]
}

// resolvedAgent is the agent the token endpoint would hand the workload.
func resolvedAgent(t *testing.T, ctx context.Context, ti *testInstance, admission *types.WorkloadAdmission) uuid.UUID {
	t.Helper()

	agentID, ok, err := workloadidentity.ResolveAssignedAgent(ctx, ti.conn, workloadidentity.AssignmentParams{
		OrganizationID:   ti.orgID,
		WorkloadIssuerID: uuid.MustParse(admission.WorkloadIssuerID),
		Subject:          admission.Subject,
	})
	require.NoError(t, err)
	require.True(t, ok, "the workload has no agent assigned")

	return agentID
}

func TestUpdateSubject_RelabelsAndLeavesTheRestUnchanged(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	original := admitLabelled(t, ctx, ti, agentID)

	payload := updateSubjectPayload(original.ID)
	payload.Name = new("  Release channel  ")
	policy, err := ti.service.UpdateSubject(ctx, payload)
	require.NoError(t, err)

	updated := onlyAdmission(t, policy)
	require.Equal(t, "Release channel", updated.Name)
	require.Equal(t, original.Tags, updated.Tags)
	require.Equal(t, original.AgentID, updated.AgentID)
	require.Equal(t, original.Subject, updated.Subject)
	require.Equal(t, original.MatchKind, updated.MatchKind)
	require.Equal(t, original.WorkloadIssuerID, updated.WorkloadIssuerID)
	require.Equal(t, original.ProjectID, updated.ProjectID)
}

func TestUpdateSubject_ClearsABlankName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	original := admitLabelled(t, ctx, ti, agentID)

	payload := updateSubjectPayload(original.ID)
	payload.Name = new("   ")
	policy, err := ti.service.UpdateSubject(ctx, payload)
	require.NoError(t, err)

	require.Empty(t, onlyAdmission(t, policy).Name)
}

func TestUpdateSubject_ReplacesTheTags(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	original := admitLabelled(t, ctx, ti, agentID)

	payload := updateSubjectPayload(original.ID)
	payload.Tags = []string{"  staging ", "ci", "staging"}
	policy, err := ti.service.UpdateSubject(ctx, payload)
	require.NoError(t, err)

	updated := onlyAdmission(t, policy)
	require.Equal(t, []string{"staging", "ci"}, updated.Tags)
	require.Equal(t, original.Name, updated.Name)
}

func TestUpdateSubject_ClearsTheTagsWithAnEmptyList(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	original := admitLabelled(t, ctx, ti, agentID)

	payload := updateSubjectPayload(original.ID)
	payload.Tags = []string{}
	policy, err := ti.service.UpdateSubject(ctx, payload)
	require.NoError(t, err)

	updated := onlyAdmission(t, policy)
	require.NotNil(t, updated.Tags)
	require.Empty(t, updated.Tags)
}

func TestUpdateSubject_WithNoFieldsLeavesTheAdmissionUnchanged(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	original := admitLabelled(t, ctx, ti, agentID)

	policy, err := ti.service.UpdateSubject(ctx, updateSubjectPayload(original.ID))
	require.NoError(t, err)

	updated := onlyAdmission(t, policy)
	require.Equal(t, original.Name, updated.Name)
	require.Equal(t, original.Tags, updated.Tags)
	require.Equal(t, original.AgentID, updated.AgentID)
}

func TestUpdateSubject_ReassignsTheAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	firstAgent := newAgent(t, ctx, ti, "claude-tag-poc")
	secondAgent := newAgent(t, ctx, ti, "claude-tag-prod")
	original := admitLabelled(t, ctx, ti, firstAgent)

	payload := updateSubjectPayload(original.ID)
	payload.AgentID = new(secondAgent.String())
	policy, err := ti.service.UpdateSubject(ctx, payload)
	require.NoError(t, err)

	updated := onlyAdmission(t, policy)
	require.Equal(t, secondAgent.String(), updated.AgentID)
	require.Equal(t, "claude-tag-prod", updated.AgentName)
	require.Equal(t, original.Name, updated.Name)

	// What the token endpoint reads, not only what the list renders.
	require.Equal(t, secondAgent, resolvedAgent(t, ctx, ti, updated))
}

func TestUpdateSubject_ReassigningOneTierReassignsTheSharedAssignment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	firstAgent := newAgent(t, ctx, ti, "claude-tag-poc")
	secondAgent := newAgent(t, ctx, ti, "claude-tag-prod")

	// The same tuple admitted at both tiers shares one assignment, which is
	// keyed on (issuer, match_kind, subject) while an admission is tiered.
	orgTier := admitLabelled(t, ctx, ti, firstAgent)
	_, err := ti.service.AdmitSubject(ctx, &gen.AdmitSubjectPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Issuer:           anthropicIssuer,
		Subject:          channelOne,
		MatchKind:        string(workloadidentity.MatchKindExact),
		Name:             nil,
		Tags:             nil,
		AgentID:          firstAgent.String(),
		ProjectScoped:    true,
	})
	require.NoError(t, err)

	payload := updateSubjectPayload(orgTier.ID)
	payload.AgentID = new(secondAgent.String())
	policy, err := ti.service.UpdateSubject(ctx, payload)
	require.NoError(t, err)

	require.Len(t, policy.Admissions, 2)
	for _, admission := range policy.Admissions {
		require.Equal(t, secondAgent.String(), admission.AgentID,
			"the admission at tier %q did not follow the shared assignment", admission.ProjectID)
	}
	require.Equal(t, secondAgent, resolvedAgent(t, ctx, ti, orgTier))
}

func TestUpdateSubject_RefusesInvalidFields(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	original := admitLabelled(t, ctx, ti, agentID)

	for _, tc := range []struct {
		name  string
		apply func(*gen.UpdateSubjectPayload)
	}{
		{name: "name with a nul character", apply: func(p *gen.UpdateSubjectPayload) { p.Name = new("a\x00b") }},
		{name: "blank tag", apply: func(p *gen.UpdateSubjectPayload) { p.Tags = []string{"production", "  "} }},
		{name: "overlong tag", apply: func(p *gen.UpdateSubjectPayload) { p.Tags = []string{strings.Repeat("a", 65)} }},
		{name: "invalid agent id", apply: func(p *gen.UpdateSubjectPayload) { p.AgentID = new("not-a-uuid") }},
		{name: "invalid id", apply: func(p *gen.UpdateSubjectPayload) { p.ID = "not-a-uuid" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			payload := updateSubjectPayload(original.ID)
			tc.apply(payload)
			_, err := ti.service.UpdateSubject(ctx, payload)
			requireOopsCode(t, err, oops.CodeInvalid)
		})
	}

	policy, err := ti.service.List(ctx, &gen.ListPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, original, onlyAdmission(t, policy))
}

func TestUpdateSubject_RefusesMoreThanFortyTags(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	original := admitLabelled(t, ctx, ti, agentID)

	tags := make([]string, 41)
	for i := range tags {
		tags[i] = uuid.NewString()
	}

	payload := updateSubjectPayload(original.ID)
	payload.Tags = tags
	_, err := ti.service.UpdateSubject(ctx, payload)
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpdateSubject_RefusesAnUnknownAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	original := admitLabelled(t, ctx, ti, agentID)

	payload := updateSubjectPayload(original.ID)
	payload.AgentID = new(uuid.NewString())
	payload.Name = new("Release channel")
	_, err := ti.service.UpdateSubject(ctx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)

	// Refused as a whole: the label is not saved without the agent.
	policy, err := ti.service.List(ctx, &gen.ListPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, original, onlyAdmission(t, policy))
}

func TestUpdateSubject_RefusesADeletedAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	deletedAgent := newAgent(t, ctx, ti, "retired")
	_, err := agentsRepo.New(ti.conn).DeleteAgent(ctx, agentsRepo.DeleteAgentParams{
		OrganizationID: ti.orgID,
		ID:             deletedAgent,
	})
	require.NoError(t, err)

	original := admitLabelled(t, ctx, ti, agentID)

	payload := updateSubjectPayload(original.ID)
	payload.AgentID = new(deletedAgent.String())
	_, err = ti.service.UpdateSubject(ctx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Equal(t, agentID, resolvedAgent(t, ctx, ti, original))
}

func TestUpdateSubject_RefusesAnotherOrganizationsAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	original := admitLabelled(t, ctx, ti, agentID)

	otherOrg := "org_" + uuid.NewString()
	_, err := ti.conn.Exec( //nolint:glint // notestingrawsql: another organization's rows are not reachable through the caller's API
		ctx, `INSERT INTO organization_metadata (id, name, slug) VALUES ($1, 'other', $1)`, otherOrg)
	require.NoError(t, err)
	_, err = ti.conn.Exec( //nolint:glint // notestingrawsql: see above; an agent's owner must be a member of its organization
		ctx, `INSERT INTO organization_user_relationships (organization_id, user_id) VALUES ($1, $2)`, otherOrg, ti.userID)
	require.NoError(t, err)

	otherAgent, err := agentsRepo.New(ti.conn).CreateAgent(ctx, agentsRepo.CreateAgentParams{
		OrganizationID: otherOrg,
		OwnerUserID:    ti.userID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Name:           "someone-elses",
	})
	require.NoError(t, err)

	payload := updateSubjectPayload(original.ID)
	payload.AgentID = new(otherAgent.ID.String())
	_, err = ti.service.UpdateSubject(ctx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Equal(t, agentID, resolvedAgent(t, ctx, ti, original))
}

func TestUpdateSubject_RefusesAnAdmissionOutsideTheCallersTenancy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	otherOrg := "org_" + uuid.NewString()
	_, err := ti.conn.Exec( //nolint:glint // notestingrawsql: another organization's rows are not reachable through the caller's API
		ctx, `INSERT INTO organization_metadata (id, name, slug) VALUES ($1, 'other', $1)`, otherOrg)
	require.NoError(t, err)

	var otherAdmission uuid.UUID
	err = ti.conn.QueryRow( //nolint:glint // notestingrawsql: see above
		ctx, `
		WITH issuer AS (
		  INSERT INTO workload_issuers (organization_id, name, issuer, jwks_uri)
		  VALUES ($1, 'other-ci', 'https://other.example.com', 'https://other.example.com/jwks')
		  RETURNING id
		)
		INSERT INTO workload_identity_admissions (organization_id, workload_issuer_id, subject, name)
		SELECT $1, id, 'repo:other/app', 'theirs' FROM issuer
		RETURNING id
	`, otherOrg).Scan(&otherAdmission)
	require.NoError(t, err)

	for _, id := range []string{otherAdmission.String(), uuid.NewString()} {
		payload := updateSubjectPayload(id)
		payload.Name = new("taken over")
		_, err = ti.service.UpdateSubject(ctx, payload)
		requireOopsCode(t, err, oops.CodeNotFound)
	}

	var name string
	err = ti.conn.QueryRow( //nolint:glint // notestingrawsql: asserting the row the API must not have touched
		ctx, `SELECT name FROM workload_identity_admissions WHERE id = $1`, otherAdmission).Scan(&name)
	require.NoError(t, err)
	require.Equal(t, "theirs", name)
}

func TestUpdateSubject_RefusesAWithdrawnAdmission(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	original := admitLabelled(t, ctx, ti, agentID)

	_, err := ti.service.WithdrawSubject(ctx, &gen.WithdrawSubjectPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               original.ID,
	})
	require.NoError(t, err)

	payload := updateSubjectPayload(original.ID)
	payload.AgentID = new(agentID.String())
	_, err = ti.service.UpdateSubject(ctx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)

	// No assignment is recreated for a withdrawn admission.
	_, ok, err := workloadidentity.ResolveAssignedAgent(ctx, ti.conn, workloadidentity.AssignmentParams{
		OrganizationID:   ti.orgID,
		WorkloadIssuerID: uuid.MustParse(original.WorkloadIssuerID),
		Subject:          original.Subject,
	})
	require.NoError(t, err)
	require.False(t, ok)
}

func TestUpdateSubject_RequiresWorkloadWrite(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "claude-tag-poc")
	original := admitLabelled(t, ctx, ti, agentID)

	readOnly := withScopes(t, ctx, ti, authz.ScopeWorkloadRead)
	payload := updateSubjectPayload(original.ID)
	payload.Name = new("Release channel")
	_, err := ti.service.UpdateSubject(readOnly, payload)
	requireOopsCode(t, err, oops.CodeForbidden)

	policy, err := ti.service.List(readOnly, &gen.ListPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, "Deploy channel", onlyAdmission(t, policy).Name)
}

func TestUpdateSubject_RecordsTheChangeAsADiff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	firstAgent := newAgent(t, ctx, ti, "claude-tag-poc")
	secondAgent := newAgent(t, ctx, ti, "claude-tag-prod")
	original := admitLabelled(t, ctx, ti, firstAgent)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionUpdate)
	require.NoError(t, err)

	payload := updateSubjectPayload(original.ID)
	payload.Name = new("Release channel")
	payload.Tags = []string{"staging"}
	payload.AgentID = new(secondAgent.String())
	_, err = ti.service.UpdateSubject(ctx, payload)
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionUpdate)
	require.NoError(t, err)
	require.Equal(t, original.ID, entry.SubjectID)
	require.Equal(t, "workload_admission", entry.SubjectType)
	require.Equal(t, channelOne, entry.SubjectDisplay)
	require.Equal(t, ti.orgID, entry.OrganizationID)

	beforeSnapshot, err := audittest.DecodeAuditData(entry.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, "Deploy channel", beforeSnapshot["name"])
	require.Equal(t, []any{"production", "slack"}, beforeSnapshot["tags"])
	require.Equal(t, firstAgent.String(), beforeSnapshot["assigned_agent_id"])
	require.Equal(t, "organization", beforeSnapshot["tier"])

	afterSnapshot, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "Release channel", afterSnapshot["name"])
	require.Equal(t, []any{"staging"}, afterSnapshot["tags"])
	require.Equal(t, secondAgent.String(), afterSnapshot["assigned_agent_id"])
	require.Equal(t, channelOne, afterSnapshot["subject"])
	require.Equal(t, "exact", afterSnapshot["match_kind"])
	require.Equal(t, anthropicIssuer, afterSnapshot["issuer"])
}

func TestUpdateSubject_RollsBackWhenTheAuditEntryFails(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, true)
	firstAgent := newAgent(t, ctx, ti, "claude-tag-poc")
	secondAgent := newAgent(t, ctx, ti, "claude-tag-prod")
	original := admitLabelled(t, ctx, ti, firstAgent)
	require.NoError(t, audittest.RejectAction(ctx, ti.conn, audit.ActionWorkloadAdmissionUpdate))

	payload := updateSubjectPayload(original.ID)
	payload.Name = new("Release channel")
	payload.AgentID = new(secondAgent.String())
	_, err := ti.service.UpdateSubject(ctx, payload)
	requireOopsCode(t, err, oops.CodeUnexpected)

	policy, err := ti.service.List(ctx, &gen.ListPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, original, onlyAdmission(t, policy))
	require.Equal(t, firstAgent, resolvedAgent(t, ctx, ti, original))
}
