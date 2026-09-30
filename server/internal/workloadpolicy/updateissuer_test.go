package workloadpolicy_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// registerDescribedAnthropic registers an issuer with every editable field set,
// so a test can tell an unchanged field from one reset to its default.
func registerDescribedAnthropic(t *testing.T, ctx context.Context, ti *testInstance) *types.WorkloadIssuer {
	t.Helper()

	policy, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "Claude Tag",
		Issuer:                 anthropicIssuer,
		JwksURI:                anthropicJWKS,
		Description:            new("Claude agents in our Slack workspace"),
		AllowWildcardAdmission: new(true),
		Tags:                   []string{"production", "slack"},
		ProjectScoped:          false,
	})
	require.NoError(t, err)
	require.Len(t, policy.Issuers, 1)

	return policy.Issuers[0]
}

func updatePayload(id string) *gen.UpdateIssuerPayload {
	return &gen.UpdateIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               id,
		Name:             nil,
		JwksURI:          nil,
		Description:      nil,
		Tags:             nil,
	}
}

func onlyIssuer(t *testing.T, policy *gen.WorkloadIdentityPolicy) *types.WorkloadIssuer {
	t.Helper()
	require.Len(t, policy.Issuers, 1)
	return policy.Issuers[0]
}

func TestUpdateIssuer_RenamesAndLeavesTheRestUnchanged(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	original := registerDescribedAnthropic(t, ctx, ti)

	payload := updatePayload(original.ID)
	payload.Name = new("  Claude in Slack  ")
	policy, err := ti.service.UpdateIssuer(ctx, payload)
	require.NoError(t, err)

	updated := onlyIssuer(t, policy)
	require.Equal(t, "Claude in Slack", updated.Name)
	require.Equal(t, original.Description, updated.Description)
	require.Equal(t, original.Tags, updated.Tags)
	require.Equal(t, original.JwksURI, updated.JwksURI)
	require.Equal(t, original.Issuer, updated.Issuer)
	require.Equal(t, original.AllowWildcardAdmission, updated.AllowWildcardAdmission)
	require.Equal(t, original.ProjectID, updated.ProjectID)
}

func TestUpdateIssuer_ChangesTheDescription(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	original := registerDescribedAnthropic(t, ctx, ti)

	payload := updatePayload(original.ID)
	payload.Description = new("  Claude agents across every workspace  ")
	policy, err := ti.service.UpdateIssuer(ctx, payload)
	require.NoError(t, err)

	updated := onlyIssuer(t, policy)
	require.Equal(t, "Claude agents across every workspace", updated.Description)
	require.Equal(t, original.Name, updated.Name)
	require.Equal(t, original.Tags, updated.Tags)
}

func TestUpdateIssuer_ClearsABlankDescription(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	original := registerDescribedAnthropic(t, ctx, ti)

	payload := updatePayload(original.ID)
	payload.Description = new("   ")
	policy, err := ti.service.UpdateIssuer(ctx, payload)
	require.NoError(t, err)

	require.Empty(t, onlyIssuer(t, policy).Description)
}

func TestUpdateIssuer_ReplacesTheTags(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	original := registerDescribedAnthropic(t, ctx, ti)

	payload := updatePayload(original.ID)
	payload.Tags = []string{"  staging ", "ci", "staging"}
	policy, err := ti.service.UpdateIssuer(ctx, payload)
	require.NoError(t, err)

	updated := onlyIssuer(t, policy)
	require.Equal(t, []string{"staging", "ci"}, updated.Tags)
	require.Equal(t, original.Description, updated.Description)
}

func TestUpdateIssuer_ClearsTheTagsWithAnEmptyList(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	original := registerDescribedAnthropic(t, ctx, ti)

	payload := updatePayload(original.ID)
	payload.Tags = []string{}
	policy, err := ti.service.UpdateIssuer(ctx, payload)
	require.NoError(t, err)

	updated := onlyIssuer(t, policy)
	require.NotNil(t, updated.Tags)
	require.Empty(t, updated.Tags)
}

func TestUpdateIssuer_RepointsTheJWKSURI(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	original := registerDescribedAnthropic(t, ctx, ti)

	payload := updatePayload(original.ID)
	payload.JwksURI = new("https://keys.anthropic.com/jwks.json")
	policy, err := ti.service.UpdateIssuer(ctx, payload)
	require.NoError(t, err)

	updated := onlyIssuer(t, policy)
	require.Equal(t, "https://keys.anthropic.com/jwks.json", updated.JwksURI)
	require.Equal(t, anthropicIssuer, updated.Issuer)
}

func TestUpdateIssuer_WithNoFieldsLeavesTheIssuerUnchanged(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	original := registerDescribedAnthropic(t, ctx, ti)

	policy, err := ti.service.UpdateIssuer(ctx, updatePayload(original.ID))
	require.NoError(t, err)

	updated := onlyIssuer(t, policy)
	require.Equal(t, original.Name, updated.Name)
	require.Equal(t, original.Description, updated.Description)
	require.Equal(t, original.Tags, updated.Tags)
	require.Equal(t, original.JwksURI, updated.JwksURI)
}

func TestUpdateIssuer_RefusesInvalidFields(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	original := registerDescribedAnthropic(t, ctx, ti)

	for _, tc := range []struct {
		name  string
		apply func(*gen.UpdateIssuerPayload)
	}{
		{name: "blank name", apply: func(p *gen.UpdateIssuerPayload) { p.Name = new("   ") }},
		{name: "http jwks_uri", apply: func(p *gen.UpdateIssuerPayload) { p.JwksURI = new("http://identity.anthropic.com/jwks") }},
		{name: "ip address jwks_uri", apply: func(p *gen.UpdateIssuerPayload) { p.JwksURI = new("https://10.0.0.1/jwks") }},
		{name: "single label jwks_uri", apply: func(p *gen.UpdateIssuerPayload) { p.JwksURI = new("https://internal/jwks") }},
		{name: "overlong description", apply: func(p *gen.UpdateIssuerPayload) { p.Description = new(strings.Repeat("a", 501)) }},
		{name: "description with a nul character", apply: func(p *gen.UpdateIssuerPayload) { p.Description = new("a\x00b") }},
		{name: "blank tag", apply: func(p *gen.UpdateIssuerPayload) { p.Tags = []string{"production", "  "} }},
		{name: "overlong tag", apply: func(p *gen.UpdateIssuerPayload) { p.Tags = []string{strings.Repeat("a", 65)} }},
		{name: "invalid id", apply: func(p *gen.UpdateIssuerPayload) { p.ID = "not-a-uuid" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			payload := updatePayload(original.ID)
			tc.apply(payload)
			_, err := ti.service.UpdateIssuer(ctx, payload)
			requireOopsCode(t, err, oops.CodeInvalid)
		})
	}

	policy, err := ti.service.List(ctx, &gen.ListPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, original, onlyIssuer(t, policy))
}

func TestUpdateIssuer_RefusesANameAlreadyUsedAtTheSameTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	original := registerDescribedAnthropic(t, ctx, ti)
	_, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "GitHub Actions",
		Issuer:                 "https://token.actions.githubusercontent.com",
		JwksURI:                "https://token.actions.githubusercontent.com/.well-known/jwks",
		Description:            nil,
		AllowWildcardAdmission: new(true),
		Tags:                   nil,
		ProjectScoped:          false,
	})
	require.NoError(t, err)

	payload := updatePayload(original.ID)
	payload.Name = new("GitHub Actions")
	_, err = ti.service.UpdateIssuer(ctx, payload)
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestUpdateIssuer_RefusesAnIssuerOutsideTheCallersTenancy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// An issuer in another organization.
	otherOrg := "org_" + uuid.NewString()
	_, err := ti.conn.Exec( //nolint:glint // notestingrawsql: another organization's rows are not reachable through the caller's API
		ctx, `INSERT INTO organization_metadata (id, name, slug) VALUES ($1, 'other', $1)`, otherOrg)
	require.NoError(t, err)

	var otherIssuer uuid.UUID
	err = ti.conn.QueryRow( //nolint:glint // notestingrawsql: see above
		ctx, `
		INSERT INTO workload_issuers (organization_id, name, issuer, jwks_uri)
		VALUES ($1, 'other-ci', 'https://other.example.com', 'https://other.example.com/jwks')
		RETURNING id
	`, otherOrg).Scan(&otherIssuer)
	require.NoError(t, err)

	for _, id := range []string{otherIssuer.String(), uuid.NewString()} {
		payload := updatePayload(id)
		payload.Name = new("taken over")
		_, err = ti.service.UpdateIssuer(ctx, payload)
		requireOopsCode(t, err, oops.CodeNotFound)
	}

	var name string
	err = ti.conn.QueryRow( //nolint:glint // notestingrawsql: asserting the row the API must not have touched
		ctx, `SELECT name FROM workload_issuers WHERE id = $1`, otherIssuer).Scan(&name)
	require.NoError(t, err)
	require.Equal(t, "other-ci", name)
}

// A sibling project's issuer is invisible in the caller's list, so it must not
// be editable by supplying its UUID either.
func TestUpdateIssuer_RefusesASiblingProjectsIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	siblingProject := uuid.New()
	_, err := ti.conn.Exec( //nolint:glint // notestingrawsql: registering under another project is not reachable through the API
		ctx, `
		INSERT INTO projects (id, organization_id, name, slug)
		VALUES ($1::uuid, $2, 'sibling', 'sibling-' || left($1::uuid::text, 8))
	`, siblingProject, ti.orgID)
	require.NoError(t, err)

	var siblingIssuer uuid.UUID
	err = ti.conn.QueryRow( //nolint:glint // notestingrawsql: see above
		ctx, `
		INSERT INTO workload_issuers (organization_id, project_id, name, issuer, jwks_uri)
		VALUES ($1, $2, 'sibling-ci', 'https://sibling.example.com', 'https://sibling.example.com/jwks')
		RETURNING id
	`, ti.orgID, siblingProject).Scan(&siblingIssuer)
	require.NoError(t, err)

	payload := updatePayload(siblingIssuer.String())
	payload.Name = new("taken over")
	_, err = ti.service.UpdateIssuer(ctx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateIssuer_RefusesAWithdrawnIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	original := registerDescribedAnthropic(t, ctx, ti)
	_, err := ti.service.WithdrawIssuer(ctx, &gen.WithdrawIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               original.ID,
	})
	require.NoError(t, err)

	payload := updatePayload(original.ID)
	payload.Name = new("Claude in Slack")
	_, err = ti.service.UpdateIssuer(ctx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateIssuer_RequiresWorkloadWrite(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	original := registerDescribedAnthropic(t, ctx, ti)

	readOnly := withScopes(t, ctx, ti, authz.ScopeWorkloadRead)
	payload := updatePayload(original.ID)
	payload.Name = new("Claude in Slack")
	_, err := ti.service.UpdateIssuer(readOnly, payload)
	requireOopsCode(t, err, oops.CodeForbidden)

	policy, err := ti.service.List(readOnly, &gen.ListPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, "Claude Tag", onlyIssuer(t, policy).Name)
}

func TestUpdateIssuer_RecordsTheChangeAsADiff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	original := registerDescribedAnthropic(t, ctx, ti)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadIssuerUpdate)
	require.NoError(t, err)

	payload := updatePayload(original.ID)
	payload.Name = new("Claude in Slack")
	payload.JwksURI = new("https://keys.anthropic.com/jwks.json")
	_, err = ti.service.UpdateIssuer(ctx, payload)
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionWorkloadIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, original.ID, entry.SubjectID)
	require.Equal(t, "workload_issuer", entry.SubjectType)
	require.Equal(t, "Claude in Slack", entry.SubjectDisplay)
	require.Equal(t, ti.orgID, entry.OrganizationID)

	beforeSnapshot, err := audittest.DecodeAuditData(entry.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, "Claude Tag", beforeSnapshot["name"])
	require.Equal(t, anthropicJWKS, beforeSnapshot["jwks_uri"])
	require.Equal(t, "organization", beforeSnapshot["tier"])

	afterSnapshot, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "Claude in Slack", afterSnapshot["name"])
	require.Equal(t, "https://keys.anthropic.com/jwks.json", afterSnapshot["jwks_uri"])
	require.Equal(t, anthropicIssuer, afterSnapshot["issuer"])
	require.Equal(t, "Claude agents in our Slack workspace", afterSnapshot["description"])
}

func TestUpdateIssuer_RollsBackWhenTheAuditEntryFails(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	original := registerDescribedAnthropic(t, ctx, ti)
	require.NoError(t, audittest.RejectAction(ctx, ti.conn, audit.ActionWorkloadIssuerUpdate))

	payload := updatePayload(original.ID)
	payload.Name = new("Claude in Slack")
	_, err := ti.service.UpdateIssuer(ctx, payload)
	requireOopsCode(t, err, oops.CodeUnexpected)

	policy, err := ti.service.List(ctx, &gen.ListPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, "Claude Tag", onlyIssuer(t, policy).Name)
}
