package catalogpromotion

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/catalogpromotion/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	delivery "github.com/speakeasy-api/gram/server/internal/thirdparty/catalogpromotion"
)

type recordingDelivery struct {
	mu      sync.Mutex
	guides  []delivery.Guide
	err     error
	onGuide func(context.Context) error
}

func (r *recordingDelivery) EnsureGuide(ctx context.Context, guide delivery.Guide) (string, error) {
	r.mu.Lock()
	r.guides = append(r.guides, guide)
	err, onGuide := r.err, r.onGuide
	r.mu.Unlock()
	if onGuide != nil {
		err = onGuide(ctx)
	}
	if err != nil {
		return "", err
	}
	return "https://github.com/example/guides/issues/42", nil
}

func deliveryCandidate(t *testing.T, f promotionFixture) Candidate {
	t.Helper()
	rows, _, err := f.service.ListCandidates(t.Context(), "", 10)
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
	f.service.effectTimeout = -time.Nanosecond
	_, err := f.promote(t)
	require.NoError(t, err)
	require.Error(t, f.service.Dispatch(t.Context()))
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
	rows, _, err := f.service.ListCandidates(t.Context(), "", 10)
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
	source, err := repo.New(f.db).SeedPromotionSourceFixture(t.Context(), repo.SeedPromotionSourceFixtureParams{ProjectID: project.ID, CreatedAt: conv.ToPGTimestamptz(f.health.now.Add(-7 * 24 * time.Hour)), Url: "https://second.example.test/mcp"})
	require.NoError(t, err)
	second := f
	second.target = Target{OrganizationID: f.target.OrganizationID, ProjectID: project.ID, MCPServerID: source}
	candidate, err := f.service.GetCandidate(t.Context(), second.target)
	require.NoError(t, err)
	second.request.Target = second.target
	second.request.SourceRevision = candidate.SourceRevision
	second.request.RetryKey = "second-synthetic-retry"
	second.request.Public.URL = "https://second.example.test/mcp"
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
	rows, _, err := f.service.ListCandidates(t.Context(), "", 10)
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
