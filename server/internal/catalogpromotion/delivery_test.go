package catalogpromotion

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/catalogpromotion/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	delivery "github.com/speakeasy-api/gram/server/internal/thirdparty/catalogpromotion"
)

type recordingDelivery struct {
	mu     sync.Mutex
	guides []delivery.Guide
	err    error
	// result overrides the default filed guide request when its outcome is set.
	result  delivery.GuideResult
	onGuide func(context.Context) error
}

func (r *recordingDelivery) EnsureGuide(ctx context.Context, guide delivery.Guide) (delivery.GuideResult, error) {
	r.mu.Lock()
	r.guides = append(r.guides, guide)
	err, onGuide, result := r.err, r.onGuide, r.result
	r.mu.Unlock()
	if onGuide != nil {
		err = onGuide(ctx)
	}
	if err != nil {
		return delivery.GuideResult{}, err
	}
	if result.Outcome != "" {
		return result, nil
	}
	return delivery.GuideResult{URL: "https://github.com/example/guides/issues/42", Outcome: delivery.OutcomeRequested}, nil
}

var (
	existingGuide = delivery.GuideResult{URL: "https://github.com/example/guides/tree/HEAD/guides/synthetic-mcp", Outcome: delivery.OutcomeExistingGuide}
	guideInReview = delivery.GuideResult{URL: "https://github.com/example/guides/pull/7", Outcome: delivery.OutcomeGuideInReview}
)

func promotionRow(t *testing.T, f promotionFixture, candidate Candidate) repo.RegistryPromotionCandidate {
	t.Helper()
	id, err := uuid.Parse(candidate.ID)
	require.NoError(t, err)
	row, err := repo.New(f.db).GetPromotion(t.Context(), repo.GetPromotionParams{OrganizationID: f.target.OrganizationID, ID: id})
	require.NoError(t, err)
	return row
}

func TestDispatchRecordsExistingGuideWithoutRequest(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := &recordingDelivery{result: existingGuide}
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	require.NoError(t, f.service.Dispatch(t.Context()))
	candidate := deliveryCandidate(t, f)
	require.Equal(t, "existing_guide", candidate.DocsStatus)
	require.Equal(t, existingGuide.URL, candidate.DocsURL)
	require.NotEmpty(t, candidate.DocsQueuedAt)
	require.Empty(t, candidate.DeliveryError)
	require.False(t, candidate.DocsRetryAllowed)
	f.service.now = func() time.Time { return f.health.now.Add(30 * 24 * time.Hour) }
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Len(t, r.guides, 1, "an existing guide is a final outcome")
	status, err := f.service.Status(t.Context())
	require.NoError(t, err)
	require.Zero(t, status.PendingDocs)
	require.Zero(t, status.GuideInReview)
}

func TestDispatchWaitsForOpenGuidePullRequestThenRecordsMergedGuide(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := &recordingDelivery{result: guideInReview}
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	require.NoError(t, f.service.Dispatch(t.Context()))
	candidate := deliveryCandidate(t, f)
	require.Equal(t, "guide_in_review", candidate.DocsStatus)
	require.Equal(t, guideInReview.URL, candidate.DocsURL)
	require.Equal(t, 1, candidate.DocsAttempts)
	require.Empty(t, candidate.DeliveryError)
	require.Empty(t, candidate.DocsQueuedAt)
	require.False(t, candidate.DocsRetryAllowed, "a scheduled recheck is not a failure to retry")
	next, err := time.Parse(time.RFC3339, candidate.DocsNextAttemptAt)
	require.NoError(t, err)
	require.False(t, next.Before(f.health.now.Add(guideReviewRecheck).Truncate(time.Second)))
	row := promotionRow(t, f, candidate)
	require.True(t, row.DocsReviewSince.Valid)
	require.Zero(t, row.DocsFailures, "waiting on a review must not count towards exhausted retries")

	status, err := f.service.Status(t.Context())
	require.NoError(t, err)
	require.Zero(t, status.PendingDocs, "a guide in review must not trip the delivery backlog alert")
	require.True(t, status.OldestPendingAt.IsZero())
	require.EqualValues(t, 1, status.GuideInReview)

	f.service.now = func() time.Time { return f.health.now.Add(time.Hour) }
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Len(t, r.guides, 1, "the recheck waits for its schedule")

	r.result = existingGuide
	f.service.now = func() time.Time { return next.Add(time.Second) }
	require.NoError(t, f.service.Dispatch(t.Context()))
	candidate = deliveryCandidate(t, f)
	require.Equal(t, "existing_guide", candidate.DocsStatus)
	require.Equal(t, existingGuide.URL, candidate.DocsURL)
	require.False(t, promotionRow(t, f, candidate).DocsReviewSince.Valid)
}

func TestDispatchRequestsGuideAfterUnmergedGuidePullRequest(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := &recordingDelivery{result: guideInReview}
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	require.NoError(t, f.service.Dispatch(t.Context()))
	r.result = delivery.GuideResult{}
	f.service.now = func() time.Time { return f.health.now.Add(guideReviewRecheck + time.Minute) }
	require.NoError(t, f.service.Dispatch(t.Context()))
	candidate := deliveryCandidate(t, f)
	require.Equal(t, "queued", candidate.DocsStatus)
	require.Equal(t, "https://github.com/example/guides/issues/42", candidate.DocsURL)
	require.Len(t, r.guides, 2)
}

func TestDispatchKeepsGuideReviewClockAcrossTransientFailure(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := &recordingDelivery{result: guideInReview}
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	require.NoError(t, f.service.Dispatch(t.Context()))
	since := promotionRow(t, f, deliveryCandidate(t, f)).DocsReviewSince.Time

	r.err = &delivery.DeliveryError{Kind: delivery.ErrorTransient, Code: "provider_unavailable"}
	f.service.now = func() time.Time { return f.health.now.Add(guideReviewRecheck + time.Minute) }
	require.Error(t, f.service.Dispatch(t.Context()))
	candidate := deliveryCandidate(t, f)
	require.Equal(t, "retrying", candidate.DocsStatus, "a failed recheck must not report the guide as in review")
	require.True(t, promotionRow(t, f, candidate).DocsReviewSince.Time.Equal(since))

	r.err = nil
	next, err := time.Parse(time.RFC3339, candidate.DocsNextAttemptAt)
	require.NoError(t, err)
	f.service.now = func() time.Time { return next.Add(time.Second) }
	require.NoError(t, f.service.Dispatch(t.Context()))
	candidate = deliveryCandidate(t, f)
	require.Equal(t, "guide_in_review", candidate.DocsStatus)
	require.True(t, promotionRow(t, f, candidate).DocsReviewSince.Time.Equal(since), "rechecks keep the first in-review time")
}

func TestDispatchParksStalledGuideReview(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := &recordingDelivery{result: guideInReview}
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	require.NoError(t, f.service.Dispatch(t.Context()))

	f.service.now = func() time.Time { return f.health.now.Add(guideReviewStallAfter + time.Hour) }
	require.Error(t, f.service.Dispatch(t.Context()))
	candidate := deliveryCandidate(t, f)
	require.Equal(t, "needs_attention", candidate.DocsStatus)
	require.Equal(t, codeGuideReviewStalled, candidate.DeliveryError)
	require.Equal(t, guideInReview.URL, candidate.DocsURL, "staff need the stalled pull request")
	require.Empty(t, candidate.DocsNextAttemptAt)
	require.True(t, candidate.DocsRetryAllowed)
	status, err := f.service.Status(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, status.NeedsAttention)
	require.Zero(t, status.GuideInReview)

	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Len(t, r.guides, 2, "a parked request waits for staff")

	retryGuide(t, f, candidate)
	require.False(t, promotionRow(t, f, deliveryCandidate(t, f)).DocsReviewSince.Valid, "a staff retry restarts the review clock")
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Equal(t, "guide_in_review", deliveryCandidate(t, f).DocsStatus)
	require.Len(t, r.guides, 3)
}

func TestDispatchRejectsUnknownGuideOutcome(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := &recordingDelivery{result: delivery.GuideResult{URL: "https://github.com/example/guides/issues/42", Outcome: "unexpected"}}
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	require.Error(t, f.service.Dispatch(t.Context()))
	candidate := deliveryCandidate(t, f)
	require.Equal(t, "needs_attention", candidate.DocsStatus)
	require.Equal(t, "docs_permanent", candidate.DeliveryError)
	require.Empty(t, candidate.DocsURL)
}

func deliveryCandidate(t *testing.T, f promotionFixture) Candidate {
	t.Helper()
	rows, _, err := f.service.ListCandidates(t.Context(), "", 10, "all")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return rows[0]
}

func retryGuide(t *testing.T, f promotionFixture, candidate Candidate) {
	t.Helper()
	id, err := uuid.Parse(candidate.ID)
	require.NoError(t, err)
	tx := testenv.BeginTx(t, t.Context(), f.db)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(context.WithoutCancel(t.Context())) })
	_, err = f.service.RetryDocsTx(t.Context(), tx, DocsRetryRequest{OrganizationID: f.target.OrganizationID, PromotionID: id, DocsRevision: candidate.DocsRevision, RetryKey: uuid.NewString()}, "user:synthetic-staff")
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
}

func assertDeliveryLeaseReleased(t *testing.T, f promotionFixture, candidate Candidate) {
	t.Helper()
	id, err := uuid.Parse(candidate.ID)
	require.NoError(t, err)
	row, err := repo.New(f.db).GetPromotion(t.Context(), repo.GetPromotionParams{OrganizationID: f.target.OrganizationID, ID: id})
	require.NoError(t, err)
	require.False(t, row.DocsLeaseToken.Valid)
	require.False(t, row.DocsLeaseUntil.Valid)
}

func TestDispatchRetriesTransientOnlyWhenDue(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := &recordingDelivery{err: &delivery.DeliveryError{Kind: delivery.ErrorTransient, Code: "request_failed"}}
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	require.Error(t, f.service.Dispatch(t.Context()))
	candidate := deliveryCandidate(t, f)
	require.Equal(t, "retrying", candidate.DocsStatus)
	require.Equal(t, 1, candidate.DocsAttempts)
	require.Equal(t, "docs_transient", candidate.DeliveryError)
	next, err := time.Parse(time.RFC3339, candidate.DocsNextAttemptAt)
	require.NoError(t, err)
	require.False(t, next.Before(f.health.now.Add(time.Hour).Truncate(time.Second)))
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Len(t, r.guides, 1)
	r.err = nil
	f.service.now = func() time.Time { return next.Add(time.Second) }
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.NoError(t, f.service.Dispatch(t.Context()))
	candidate = deliveryCandidate(t, f)
	require.Equal(t, "queued", candidate.DocsStatus)
	require.Equal(t, 2, candidate.DocsAttempts)
	require.Equal(t, "https://github.com/example/guides/issues/42", candidate.DocsURL)
	require.Len(t, r.guides, 2)
	encoded, err := json.Marshal(r.guides[0])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), f.target.OrganizationID)
	require.NotContains(t, string(encoded), f.target.ProjectID.String())
	require.NotContains(t, string(encoded), f.target.MCPServerID.String())
	require.Equal(t, f.request.Public.URL, r.guides[0].RemoteURL)
}

func TestDispatchHonorsProviderRetryAfter(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := &recordingDelivery{err: &delivery.DeliveryError{Kind: delivery.ErrorTransient, RetryAfter: 5 * time.Hour, Code: "rate_limited"}}
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	require.Error(t, f.service.Dispatch(t.Context()))
	candidate := deliveryCandidate(t, f)
	next, err := time.Parse(time.RFC3339, candidate.DocsNextAttemptAt)
	require.NoError(t, err)
	require.False(t, next.Before(f.health.now.Add(5*time.Hour).Truncate(time.Second)))
	f.service.now = func() time.Time { return f.health.now.Add(2 * time.Hour) }
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Len(t, r.guides, 1)
}

func TestDispatchPermanentFailureNeedsManualRetry(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := &recordingDelivery{err: &delivery.DeliveryError{Kind: delivery.ErrorPermanent, Code: "marker_conflict"}}
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	require.Error(t, f.service.Dispatch(t.Context()))
	candidate := deliveryCandidate(t, f)
	require.Equal(t, "needs_attention", candidate.DocsStatus)
	require.Equal(t, "docs_permanent", candidate.DeliveryError)
	require.Empty(t, candidate.DocsNextAttemptAt)
	require.True(t, candidate.DocsRetryAllowed)
	f.service.now = func() time.Time { return f.health.now.Add(48 * time.Hour) }
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Len(t, r.guides, 1)
	retryGuide(t, f, candidate)
	r.err = nil
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Equal(t, "queued", deliveryCandidate(t, f).DocsStatus)
	require.Len(t, r.guides, 2)
}

func TestDispatchMissingConfigurationNeedsAttention(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := &recordingDelivery{err: delivery.ErrNotConfigured}
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	require.Error(t, f.service.Dispatch(t.Context()))
	candidate := deliveryCandidate(t, f)
	require.Equal(t, "needs_attention", candidate.DocsStatus)
	require.Equal(t, "docs_not_configured", candidate.DeliveryError)
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Len(t, r.guides, 1)
}

func TestDispatchEffectDeadlineRetainsRetryReceipt(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := &recordingDelivery{onGuide: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	f.service.delivery = r
	f.service.effectTimeout = time.Second
	_, err := f.promote(t)
	require.NoError(t, err)
	require.Error(t, f.service.Dispatch(t.Context()))
	require.Len(t, r.guides, 1, "deadline must interrupt the external delivery call")
	candidate := deliveryCandidate(t, f)
	require.Equal(t, "retrying", candidate.DocsStatus)
	require.Equal(t, 1, candidate.DocsAttempts)
	require.NotEmpty(t, candidate.DocsLastAttemptAt)
	require.NotEmpty(t, candidate.DocsNextAttemptAt)
	require.False(t, candidate.DocsRetryAllowed, "manual retries must respect scheduled backoff")
	assertDeliveryLeaseReleased(t, f, candidate)
	next, err := time.Parse(time.RFC3339, candidate.DocsNextAttemptAt)
	require.NoError(t, err)
	f.service.now = func() time.Time { return next.Add(time.Second) }
	retryGuide(t, f, candidate)
	r.onGuide = nil
	f.service.effectTimeout = time.Minute
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Equal(t, "queued", deliveryCandidate(t, f).DocsStatus)
}

func TestConcurrentDispatchClaimsGuideOnce(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	r := &recordingDelivery{onGuide: func(ctx context.Context) error {
		once.Do(func() { close(started) })
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() { result <- f.service.Dispatch(t.Context()) }()
	<-started
	secondErr := f.service.Dispatch(t.Context())
	close(release)
	require.NoError(t, <-result)
	require.NoError(t, secondErr)
	require.Len(t, r.guides, 1)
}

func TestDispatchCandidateDoesNotQueueDocs(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := new(recordingDelivery)
	f.service.delivery = r
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Empty(t, r.guides)
}

func TestDispatchRetainsApprovedDeliveryAfterProjectDeletion(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := new(recordingDelivery)
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	require.NoError(t, repo.New(f.db).DeletePromotionProjectFixture(t.Context(), f.target.ProjectID))
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Len(t, r.guides, 1)
	require.Equal(t, "queued", deliveryCandidate(t, f).DocsStatus)
}

func TestDispatchIgnoresUnpromotedOrphanCandidate(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := new(recordingDelivery)
	f.service.delivery = r
	require.NoError(t, repo.New(f.db).DeletePromotionProjectFixture(t.Context(), f.target.ProjectID))
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Empty(t, r.guides)
	rows, _, err := f.service.ListCandidates(t.Context(), "", 10, "all")
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestDispatchDoesNotReclassifyRemovedCatalogueEntry(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := new(recordingDelivery)
	f.service.delivery = r
	promoted, err := f.promote(t)
	require.NoError(t, err)
	id, err := uuid.Parse(promoted.RegistryEntryID)
	require.NoError(t, err)
	require.NoError(t, repo.New(f.db).DeletePromotionRegistryEntryFixture(t.Context(), id))
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Len(t, r.guides, 1)
	require.Equal(t, "promoted", deliveryCandidate(t, f).Status)
}

func TestDispatchCancelledCallerDoesNotStartEffect(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := new(recordingDelivery)
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, f.service.Dispatch(ctx), context.Canceled)
	require.Empty(t, r.guides)
	require.Zero(t, deliveryCandidate(t, f).DocsAttempts)
}

func TestDispatchCancelledEffectPersistsFailureReceipt(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	r := &recordingDelivery{onGuide: func(context.Context) error { cancel(); return context.Canceled }}
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	err = f.service.Dispatch(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	candidate := deliveryCandidate(t, f)
	require.Equal(t, 1, candidate.DocsAttempts)
	require.Equal(t, "retrying", candidate.DocsStatus)
	require.NotEmpty(t, candidate.DocsNextAttemptAt)
	require.False(t, candidate.DocsRetryAllowed)
	assertDeliveryLeaseReleased(t, f, candidate)
}

func TestDispatchCancelledEffectDoesNotStartNextGuide(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	_, err := f.promote(t)
	require.NoError(t, err)
	project, err := projectrepo.New(f.db).CreateProject(t.Context(), projectrepo.CreateProjectParams{Name: "Second synthetic", Slug: "second-synthetic", OrganizationID: f.target.OrganizationID})
	require.NoError(t, err)
	source, err := repo.New(f.db).SeedPromotionSourceFixture(t.Context(), repo.SeedPromotionSourceFixtureParams{ProjectID: project.ID, CreatedAt: conv.ToPGTimestamptz(f.health.now.Add(-7 * 24 * time.Hour)), Url: "https://second.example.com/mcp"})
	require.NoError(t, err)
	second := f
	second.target = Target{OrganizationID: f.target.OrganizationID, ProjectID: project.ID, MCPServerID: source}
	candidate, err := f.service.GetCandidate(t.Context(), second.target)
	require.NoError(t, err)
	second.request.Target = second.target
	second.request.SourceRevision = candidate.SourceRevision
	second.request.CurationRevision = candidate.CurationRevision
	second.request.RetryKey = "second-synthetic-retry"
	second.request.Public.URL = "https://second.example.com/mcp"
	second.request.Public.Name = "io.example/second"
	second.request.Public.DocsSlug = "second-synthetic"
	_, err = second.promote(t)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	r := &recordingDelivery{onGuide: func(context.Context) error { cancel(); return context.Canceled }}
	f.service.delivery = r
	require.ErrorIs(t, f.service.Dispatch(ctx), context.Canceled)
	require.Len(t, r.guides, 1)
	rows, _, err := f.service.ListCandidates(t.Context(), "", 10, "all")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, 1, rows[0].DocsAttempts+rows[1].DocsAttempts)
}

func TestDispatchParksExhaustedRetriesAndPreservesProviderEmbargo(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := &recordingDelivery{err: &delivery.DeliveryError{Kind: delivery.ErrorTransient, Code: "rate_limited", RetryAfter: 48 * time.Hour}}
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	for attempt := 1; attempt <= maxDocsFailures; attempt++ {
		require.Error(t, f.service.Dispatch(t.Context()))
		candidate := deliveryCandidate(t, f)
		require.Equal(t, attempt, candidate.DocsAttempts)
		require.False(t, candidate.DocsRetryAllowed)
		next, err := time.Parse(time.RFC3339, candidate.DocsNextAttemptAt)
		require.NoError(t, err)
		require.False(t, next.Before(f.service.now().Add(48*time.Hour).Truncate(time.Second)))
		if attempt == maxDocsFailures {
			require.Equal(t, "needs_attention", candidate.DocsStatus)
			require.Equal(t, "docs_attempts_exhausted", candidate.DeliveryError)
		}
		f.service.now = func() time.Time { return next.Add(time.Second) }
	}
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Len(t, r.guides, maxDocsFailures)
	candidate := deliveryCandidate(t, f)
	require.True(t, candidate.DocsRetryAllowed)
	retryGuide(t, f, candidate)
	r.err = nil
	require.NoError(t, f.service.Dispatch(t.Context()))
	candidate = deliveryCandidate(t, f)
	require.Equal(t, "queued", candidate.DocsStatus)
	require.Equal(t, maxDocsFailures+1, candidate.DocsAttempts)
}

func TestDispatchManualRecoveryAfterProjectDeletion(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := &recordingDelivery{err: delivery.ErrNotConfigured}
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	require.Error(t, f.service.Dispatch(t.Context()))
	require.NoError(t, repo.New(f.db).DeletePromotionProjectFixture(t.Context(), f.target.ProjectID))
	candidate := deliveryCandidate(t, f)
	require.Empty(t, candidate.ProjectID)
	require.True(t, candidate.DocsRetryAllowed)
	retryGuide(t, f, candidate)
	r.err = nil
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Equal(t, "queued", deliveryCandidate(t, f).DocsStatus)
}

func TestDispatchReconcilesAfterProjectDeletedDuringDelivery(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	r := &recordingDelivery{onGuide: func(ctx context.Context) error {
		return repo.New(f.db).DeletePromotionProjectFixture(ctx, f.target.ProjectID)
	}}
	f.service.delivery = r
	_, err := f.promote(t)
	require.NoError(t, err)
	// Deletion changes the scoped receipt target after the external call succeeds.
	require.Error(t, f.service.Dispatch(t.Context()))
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Len(t, r.guides, 1)
	r.onGuide = nil
	f.service.now = func() time.Time { return f.health.now.Add(2 * time.Minute) }
	require.NoError(t, f.service.Dispatch(t.Context()))
	require.Len(t, r.guides, 2)
	require.Equal(t, r.guides[0], r.guides[1], "reconciliation must reuse the original public guide identity")
	require.Equal(t, "queued", deliveryCandidate(t, f).DocsStatus)
}

func TestDispatchUnwiredDeliveryDoesNotClaimOrParkJobs(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	_, err := f.promote(t)
	require.NoError(t, err)
	f.service.delivery = nil
	require.Error(t, f.service.Dispatch(t.Context()))
	candidate := deliveryCandidate(t, f)
	require.Equal(t, "pending", candidate.DocsStatus)
	require.Zero(t, candidate.DocsAttempts)
	require.Empty(t, candidate.DeliveryError)
}

type discoveryQueryCapture struct {
	contexts []context.Context
	budgets  []time.Duration
}

func (c *discoveryQueryCapture) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "-- name: DiscoveryTargets :many") {
		deadline, ok := ctx.Deadline()
		budget := time.Duration(0)
		if ok {
			budget = time.Until(deadline)
		}
		c.contexts = append(c.contexts, ctx)
		c.budgets = append(c.budgets, budget)
	}
	return ctx
}
func (*discoveryQueryCapture) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestDiscoveryRemainingQueryHasIndependentBoundedContext(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	capture := &discoveryQueryCapture{}
	config := f.db.Config()
	config.ConnConfig.Tracer = capture
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	f.service.db = pool
	batch, err := f.service.DiscoverBatch(t.Context())
	require.NoError(t, err)
	require.False(t, batch.More)
	require.Len(t, capture.contexts, 2)
	require.Positive(t, capture.budgets[1])
	require.LessOrEqual(t, capture.budgets[1], DiscoveryRecordTimeout)
	require.ErrorIs(t, capture.contexts[1].Err(), context.Canceled)
}
