package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
)

type fakePromotionReviews struct {
	actor  string
	email  string
	target catalogpromotion.Target
	err    error
}

func (f *fakePromotionReviews) StartReview(_ context.Context, target catalogpromotion.Target, actor string, actorEmail *string) (*mcpapproval.StaffReview, error) {
	f.target, f.actor, f.email = target, actor, *actorEmail
	if f.err != nil {
		return nil, f.err
	}
	return &mcpapproval.StaffReview{
		RequestID: "synthetic-review", Evidence: json.RawMessage(`{"gaps":["domain_lookup_failed"]}`), EvidenceCollectedAt: "",
		Research: &mcpapproval.StaffResearch{Status: "running", Report: nil, Error: "", StartedAt: "", CompletedAt: ""},
	}, nil
}

func TestCatalogReviewWriterStartsForVerifiedStaff(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_promotion_review")
	ctx := writeContext(t, f)
	writes := WriteConfig{Enabled: true}
	reviews := &fakePromotionReviews{}
	writer := &catalogReviewWriter{runner: f.runner, writes: writes, starter: reviews}
	input := CatalogPromotionTargetInput{OrganizationID: "org-synthetic", ProjectID: uuid.NewString(), MCPServerID: uuid.NewString()}

	receipt, err := writer.apply(ctx, input)

	require.NoError(t, err)
	require.Equal(t, "synthetic-review", receipt.Review.RequestID)
	require.Equal(t, "running", receipt.Review.Research.Status)
	require.Equal(t, map[string]any{"gaps": []any{"domain_lookup_failed"}}, receipt.Review.Evidence)
	require.Equal(t, catalogReviewSideEffects, receipt.SideEffects)
	require.Equal(t, "staff@example.test", reviews.email)
	require.NotEmpty(t, reviews.actor)
	require.Equal(t, input.OrganizationID, reviews.target.OrganizationID)
}

func TestCatalogReviewWriterRefusals(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_promotion_review_refusals")
	ctx := writeContext(t, f)
	writes := WriteConfig{Enabled: true}
	input := CatalogPromotionTargetInput{OrganizationID: "org-synthetic", ProjectID: uuid.NewString(), MCPServerID: uuid.NewString()}

	_, err := (&catalogReviewWriter{runner: f.runner, writes: WriteConfig{}, starter: &fakePromotionReviews{}}).apply(ctx, input)
	require.ErrorIs(t, err, ErrWriteDisabled)

	allowed := &catalogReviewWriter{runner: f.runner, writes: writes, starter: &fakePromotionReviews{}}
	_, err = allowed.apply(t.Context(), input)
	require.ErrorIs(t, err, ErrWriteIdentity)
	_, err = allowed.apply(ctx, CatalogPromotionTargetInput{OrganizationID: "org-synthetic", ProjectID: "not-a-uuid", MCPServerID: uuid.NewString()})
	require.Error(t, err)

	refused := &catalogReviewWriter{runner: f.runner, writes: writes, starter: &fakePromotionReviews{err: catalogpromotion.ErrReviewNotAllowed}}
	_, err = refused.apply(ctx, input)
	require.ErrorIs(t, err, catalogpromotion.ErrReviewNotAllowed)

	outage := &catalogReviewWriter{runner: f.runner, writes: writes, starter: &fakePromotionReviews{err: errors.New("synthetic outage")}}
	_, err = outage.apply(ctx, input)
	require.ErrorIs(t, err, errRegistryUnavailable)
}

func TestCatalogReviewWriterRefusesChangedConnectionsBeforeSpending(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_promotion_review_revoked")
	ctx := writeContext(t, f)
	writes := WriteConfig{Enabled: true}
	reviews := &fakePromotionReviews{}
	f.reauth()

	_, err := (&catalogReviewWriter{runner: f.runner, writes: writes, starter: reviews}).apply(ctx, CatalogPromotionTargetInput{OrganizationID: "org-synthetic", ProjectID: uuid.NewString(), MCPServerID: uuid.NewString()})

	require.ErrorIs(t, err, ErrConnectionChanged)
	require.Empty(t, reviews.actor, "no review starts on a changed connection")
}
