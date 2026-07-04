package risk_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// newPromptPolicy creates an enabled prompt_based policy and returns its id.
func newPromptPolicy(t *testing.T, ctx context.Context, ti *testInstance) string {
	t.Helper()
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagPromptPolicies, authCtx.ActiveOrganizationID, true)

	name := "Prompt Policy"
	prompt := "Flag destructive production changes."
	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:       &name,
		PolicyType: "prompt_based",
		Prompt:     &prompt,
	})
	require.NoError(t, err)
	return policy.ID
}

func grantOrgAdminCtx(t *testing.T, ctx context.Context, ti *testInstance) context.Context {
	t.Helper()
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	return withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
}

func TestRiskEvalReviews_SaveListUpsertDelete(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = grantOrgAdminCtx(t, ctx, ti)

	policyID := newPromptPolicy(t, ctx, ti)
	chatID, _ := seedChatTranscript(t, ti, *mustAuthProject(ctx), mustAuthOrg(ctx), [][2]string{
		{"user", "delete production"},
	})

	// Save a verdict.
	saved, err := ti.service.SaveRiskEvalReview(ctx, &gen.SaveRiskEvalReviewPayload{
		PolicyID: policyID,
		ChatID:   chatID.String(),
		Verdict:  "correct",
	})
	require.NoError(t, err)
	require.Equal(t, "correct", saved.Verdict)
	require.Equal(t, policyID, saved.PolicyID)
	require.Equal(t, chatID.String(), saved.ChatID)

	// List returns it.
	list, err := ti.service.ListRiskEvalReviews(ctx, &gen.ListRiskEvalReviewsPayload{PolicyID: policyID})
	require.NoError(t, err)
	require.Len(t, list.Reviews, 1)
	require.Equal(t, "correct", list.Reviews[0].Verdict)

	// Re-saving upserts (one row, updated verdict).
	_, err = ti.service.SaveRiskEvalReview(ctx, &gen.SaveRiskEvalReviewPayload{
		PolicyID: policyID,
		ChatID:   chatID.String(),
		Verdict:  "false_positive",
	})
	require.NoError(t, err)
	list, err = ti.service.ListRiskEvalReviews(ctx, &gen.ListRiskEvalReviewsPayload{PolicyID: policyID})
	require.NoError(t, err)
	require.Len(t, list.Reviews, 1)
	require.Equal(t, "false_positive", list.Reviews[0].Verdict)

	// Delete clears it.
	require.NoError(t, ti.service.DeleteRiskEvalReview(ctx, &gen.DeleteRiskEvalReviewPayload{
		PolicyID: policyID,
		ChatID:   chatID.String(),
	}))
	list, err = ti.service.ListRiskEvalReviews(ctx, &gen.ListRiskEvalReviewsPayload{PolicyID: policyID})
	require.NoError(t, err)
	require.Empty(t, list.Reviews)

	// Deleting again is a no-op.
	require.NoError(t, ti.service.DeleteRiskEvalReview(ctx, &gen.DeleteRiskEvalReviewPayload{
		PolicyID: policyID,
		ChatID:   chatID.String(),
	}))
}

func TestRiskEvalReviews_RejectsStandardPolicy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = grantOrgAdminCtx(t, ctx, ti)

	name := "Standard"
	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    &name,
		Sources: []string{"gitleaks"},
	})
	require.NoError(t, err)

	chatID, _ := seedChatTranscript(t, ti, *mustAuthProject(ctx), mustAuthOrg(ctx), [][2]string{{"user", "hi"}})
	_, err = ti.service.SaveRiskEvalReview(ctx, &gen.SaveRiskEvalReviewPayload{
		PolicyID: policy.ID,
		ChatID:   chatID.String(),
		Verdict:  "correct",
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestRiskEvalReviews_RejectsChatOutsideProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = grantOrgAdminCtx(t, ctx, ti)
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	policyID := newPromptPolicy(t, ctx, ti)
	projectSlug := "other-eval-" + uuid.New().String()[:8]
	otherProject, err := projectsRepo.New(ti.conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name:           projectSlug,
		Slug:           projectSlug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	chatID, _ := seedChatTranscript(t, ti, otherProject.ID, authCtx.ActiveOrganizationID, [][2]string{{"user", "hi"}})

	_, err = ti.service.SaveRiskEvalReview(ctx, &gen.SaveRiskEvalReviewPayload{
		PolicyID: policyID,
		ChatID:   chatID.String(),
		Verdict:  "correct",
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestRiskEvalReviews_PolicyNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = grantOrgAdminCtx(t, ctx, ti)

	missing, _ := uuid.NewV7()
	chatID, _ := seedChatTranscript(t, ti, *mustAuthProject(ctx), mustAuthOrg(ctx), [][2]string{{"user", "hi"}})
	_, err := ti.service.SaveRiskEvalReview(ctx, &gen.SaveRiskEvalReviewPayload{
		PolicyID: missing.String(),
		ChatID:   chatID.String(),
		Verdict:  "correct",
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestRiskEvalReviews_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	// Create the policy with grants, then drop them (zero-grant enterprise ctx).
	authed := grantOrgAdminCtx(t, ctx, ti)
	policyID := newPromptPolicy(t, authed, ti)

	denied := withExactAccessGrants(t, ctx, ti.conn)
	_, err := ti.service.ListRiskEvalReviews(denied, &gen.ListRiskEvalReviewsPayload{PolicyID: policyID})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func mustAuthProject(ctx context.Context) *uuid.UUID {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	return authCtx.ProjectID
}

func mustAuthOrg(ctx context.Context) string {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	return authCtx.ActiveOrganizationID
}
