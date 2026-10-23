package catalogpromotion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type orderedPromotionHealth struct {
	*fakePromotionHealth
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (h *orderedPromotionHealth) Outcomes(ctx context.Context, _ telemetry.MCPServerTelemetryTarget, _, _ time.Time) (*telemetry.MCPServerOutcomes, error) {
	if h.calls.Add(1) == 1 {
		close(h.started)
		select {
		case <-h.release:
			return &telemetry.MCPServerOutcomes{Success: 20}, nil
		case <-ctx.Done():
			return nil, fmt.Errorf("test observation canceled: %w", ctx.Err())
		}
	}
	return &telemetry.MCPServerOutcomes{Success: 20, Failed: 1}, nil
}

func TestPromotionRefreshDoesNotRestoreSupersededHealthyEvidence(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	health := &orderedPromotionHealth{fakePromotionHealth: f.health, started: make(chan struct{}), release: make(chan struct{})}
	f.service.health = health
	var ticks atomic.Int32
	f.service.now = func() time.Time { return f.health.now.Add(time.Duration(ticks.Add(1)) * time.Second) }
	type readResult struct {
		candidate Candidate
		err       error
	}
	first := make(chan readResult, 1)
	go func() {
		candidate, err := f.service.GetCandidate(t.Context(), f.target)
		first <- readResult{candidate: candidate, err: err}
	}()
	<-health.started
	newer, err := f.service.GetCandidate(t.Context(), f.target)
	require.NoError(t, err)
	require.Equal(t, "errors_observed", newer.Reason)
	close(health.release)
	older := <-first
	require.NoError(t, older.err)
	require.False(t, older.candidate.Eligible)
	require.Equal(t, newer.CheckedAt, older.candidate.CheckedAt)
	require.Equal(t, "errors_observed", older.candidate.Reason)
}

func TestPromotionExistingCatalogueEndpointIsNotCandidate(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	record, err := buildRecord(f.request.Public, "streamable-http")
	require.NoError(t, err)
	_, err = f.service.registry.Create(t.Context(), record)
	require.NoError(t, err)
	candidate, err := f.service.GetCandidate(t.Context(), f.target)
	require.NoError(t, err)
	require.False(t, candidate.Eligible)
	require.Equal(t, "already_catalogued", candidate.Reason)
	_, err = f.promote(t)
	require.ErrorIs(t, err, ErrIneligible)
}

func TestPromotionAtomicReplayAndPrivacy(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	result, err := f.promote(t)
	require.NoError(t, err)
	require.Equal(t, "promoted", result.Status)
	again, err := f.promote(t)
	require.NoError(t, err)
	require.Equal(t, result.RegistryEntryID, again.RegistryEntryID)
	audits, err := audittest.AuditLogCountByAction(t.Context(), f.db, audit.ActionMcpServerPromoteCatalogue)
	require.NoError(t, err)
	require.EqualValues(t, 1, audits)
	entry, err := f.service.registry.GetByName(t.Context(), f.request.Public.Name)
	require.NoError(t, err)
	require.True(t, entry.Published)
	require.NotContains(t, string(entry.Data), f.target.OrganizationID)
	require.NotContains(t, string(entry.Data), f.target.ProjectID.String())
	require.NotContains(t, string(entry.Data), f.target.MCPServerID.String())
	require.NotContains(t, string(entry.Data), "synthetic-staff")
	f.request.Public.Description = "Different exact change"
	_, err = f.promote(t)
	require.ErrorIs(t, err, ErrConflict)
}

func TestPromotionRollbackAndSourceRevision(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	tx := testenv.BeginTx(t, t.Context(), f.db)
	_, err := f.service.PromoteTx(t.Context(), tx, f.request, "user:synthetic-staff")
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(context.WithoutCancel(t.Context())))
	_, err = f.service.registry.GetByName(t.Context(), f.request.Public.Name)
	require.ErrorIs(t, err, mcpregistry.ErrNotFound)
	count, err := audittest.AuditLogCountByAction(t.Context(), f.db, audit.ActionMcpServerPromoteCatalogue)
	require.NoError(t, err)
	require.Zero(t, count)
	require.NoError(t, repo.New(f.db).ChangePromotionSourceFixture(t.Context(), repo.ChangePromotionSourceFixtureParams{ProjectID: f.target.ProjectID, ID: f.target.MCPServerID, Visibility: "private"}))
	_, err = f.promote(t)
	require.ErrorIs(t, err, ErrStale)
}

func TestPromotionConcurrentRetry(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	txs := []pgx.Tx{testenv.BeginTx(t, t.Context(), f.db), testenv.BeginTx(t, t.Context(), f.db)}
	for _, tx := range txs {
		wg.Go(func() {
			_, err := f.service.PromoteTx(t.Context(), tx, f.request, "user:synthetic-staff")
			if err == nil {
				err = tx.Commit(t.Context())
			}
			results <- err
		})
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	count, err := repo.New(f.db).CountPublishedPromotionFixture(t.Context(), uuid.NullUUID{UUID: f.target.ProjectID, Valid: true})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestPromotionTenantAndEvidenceBoundaries(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	wrong := f.target
	wrong.OrganizationID = "org_other_synthetic"
	_, err := f.service.GetCandidate(t.Context(), wrong)
	require.ErrorIs(t, err, ErrNotFound)
	f.service.features = fakePromotionFeatures{enabled: false}
	candidate, err := f.service.GetCandidate(t.Context(), f.target)
	require.NoError(t, err)
	require.Equal(t, "logging_disabled", candidate.Reason)
	require.False(t, candidate.Eligible)
	_, err = f.promote(t)
	require.ErrorIs(t, err, ErrIneligible)
	f.service.features = fakePromotionFeatures{enabled: true}
	f.health.failures = 1
	candidate, err = f.service.GetCandidate(t.Context(), f.target)
	require.NoError(t, err)
	require.Equal(t, "errors_observed", candidate.Reason)
	f.health.failures = 0
	_, err = f.service.GetCandidate(t.Context(), f.target)
	require.NoError(t, err)
	f.service.now = func() time.Time { return f.health.now.Add(16 * time.Minute) }
	_, err = f.promote(t)
	require.ErrorIs(t, err, ErrStale)
}

func TestPromotionPublicMetadataBoundaries(t *testing.T) {
	t.Parallel()
	valid := PublicMetadata{Name: "io.example/catalog", Title: "Synthetic", Description: "Public service", URL: "https://catalog.example.test/mcp", DocumentationURL: "https://docs.example.test/setup", DocsSlug: "synthetic-mcp", AuthMode: "oauth"}
	require.NoError(t, validatePublic(valid))
	for _, raw := range []string{"https://user:secret@example.test/mcp", "https://example.test/mcp?token=secret", "https://example.test/mcp#secret", "http://example.test/mcp", "https://127.0.0.1/mcp", "https://internal.local/mcp", "https://EXAMPLE.test/mcp", "https://example.test:443/mcp"} {
		bad := valid
		bad.URL = raw
		require.ErrorIs(t, validatePublic(bad), ErrInvalid, raw)
	}
	bad := valid
	bad.Title = strings.Repeat("x", 201)
	require.ErrorIs(t, validatePublic(bad), ErrInvalid)
	encoded, err := buildRecord(valid, "streamable-http")
	require.NoError(t, err)
	validator, err := mcpregistry.LoadValidator()
	require.NoError(t, err)
	require.Empty(t, validator.Validate(encoded))
	require.True(t, json.Valid(encoded))
}

func TestPromotionQualificationNeedsRecentMultiDaySuccess(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	outcomes := &telemetry.MCPServerOutcomes{Success: 20, Watermark: now}
	old := []telemetry.MCPServerSeriesPoint{{BucketStart: now.Add(-10 * 24 * time.Hour), Total: 10}, {BucketStart: now.Add(-9 * 24 * time.Hour), Total: 10}}
	require.Equal(t, "insufficient_evidence", qualify(outcomes, old, now).reason, "a project watermark is not server freshness")
	recent := []telemetry.MCPServerSeriesPoint{{BucketStart: now.Add(-24 * time.Hour), Total: 10}, {BucketStart: now, Total: 10}}
	require.Equal(t, "eligible", qualify(outcomes, recent, now).reason)
	recent[0].Failed = 1
	require.Equal(t, "errors_observed", qualify(outcomes, recent, now).reason, "direct evidence of failure wins over a lagging aggregate read")
	recent[0].Failed = 0
	outcomes.Unknown = 1
	require.Equal(t, "errors_observed", qualify(outcomes, recent, now).reason)
}

func TestDiscoveryFailureAdvancesScheduleAndReportsHealth(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	f.health.err = fmt.Errorf("synthetic telemetry failure")
	require.Error(t, f.service.Discover(t.Context()))
	status, err := f.service.Status(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, status.DiscoveryFailures)
	require.WithinDuration(t, f.health.now, status.LastDiscoveryAt, time.Millisecond)
	row, err := repo.New(f.db).PromotionRowFixture(t.Context(), repo.PromotionRowFixtureParams{ProjectID: uuid.NullUUID{UUID: f.target.ProjectID, Valid: true}, McpServerID: uuid.NullUUID{UUID: f.target.MCPServerID, Valid: true}})
	require.NoError(t, err)
	require.False(t, row.Eligible)
	require.EqualValues(t, 1, row.DiscoveryFailures)
	require.True(t, row.DiscoveryNextAttemptAt.Time.After(f.health.now))
	f.health.err = nil
	require.NoError(t, f.service.Discover(t.Context()))
	status, err = f.service.Status(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, status.DiscoveryFailures)
	f.service.now = func() time.Time { return f.health.now.Add(2 * time.Hour) }
	require.NoError(t, f.service.Discover(t.Context()))
	status, err = f.service.Status(t.Context())
	require.NoError(t, err)
	require.Zero(t, status.DiscoveryFailures)
}

func TestLateFailedDiscoveryCannotOverwriteNewerEvidence(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	q := repo.New(f.db)
	attempt := f.health.now
	row, err := q.ClaimDiscovery(t.Context(), repo.ClaimDiscoveryParams{OrganizationID: f.target.OrganizationID, ProjectID: f.target.ProjectID, McpServerID: f.target.MCPServerID, Now: conv.ToPGTimestamptz(attempt), NextAttemptAt: conv.ToPGTimestamptz(attempt.Add(time.Hour))})
	require.NoError(t, err)
	f.service.now = func() time.Time { return attempt.Add(time.Minute) }
	newer, err := f.service.GetCandidate(t.Context(), f.target)
	require.NoError(t, err)
	require.True(t, newer.Eligible)
	require.NoError(t, q.FinishDiscovery(t.Context(), repo.FinishDiscoveryParams{ID: row.ID, ProjectID: row.ProjectID, AttemptedAt: conv.ToPGTimestamptz(attempt), Failed: true}))
	stored, err := q.PromotionRowFixture(t.Context(), repo.PromotionRowFixtureParams{ProjectID: row.ProjectID, McpServerID: row.McpServerID})
	require.NoError(t, err)
	require.True(t, stored.Eligible)
	require.Equal(t, newer.Reason, stored.Reason)
}

func TestDocsRetryRevisionTenantAndAtomicAudit(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	promoted, err := f.promote(t)
	require.NoError(t, err)
	require.Error(t, f.service.Dispatch(t.Context()))
	rows, _, err := f.service.ListCandidates(t.Context(), "", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	parked := rows[0]
	require.Equal(t, "needs_attention", parked.DocsStatus)
	request := DocsRetryRequest{OrganizationID: f.target.OrganizationID, PromotionID: uuid.MustParse(promoted.ID), DocsRevision: parked.DocsRevision, RetryKey: "synthetic-docs-retry"}
	tx := testenv.BeginTx(t, t.Context(), f.db)
	_, err = f.service.ValidateDocsRetryTx(t.Context(), tx, "org_other_synthetic", request.PromotionID, request.DocsRevision)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = f.service.ValidateDocsRetryTx(t.Context(), tx, request.OrganizationID, request.PromotionID, promoted.DocsRevision)
	require.ErrorIs(t, err, ErrStale)
	retried, err := f.service.RetryDocsTx(t.Context(), tx, request, "user:synthetic-staff")
	require.NoError(t, err)
	require.NotEqual(t, parked.DocsRevision, retried.DocsRevision)
	require.NoError(t, tx.Commit(t.Context()))
	replayTx := testenv.BeginTx(t, t.Context(), f.db)
	replayed, err := f.service.RetryDocsTx(t.Context(), replayTx, request, "user:synthetic-staff")
	require.NoError(t, err)
	require.Equal(t, retried.DocsRevision, replayed.DocsRevision)
	require.NoError(t, replayTx.Commit(t.Context()))
	count, err := audittest.AuditLogCountByAction(t.Context(), f.db, audit.ActionCatalogPromotionRetryDocs)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}
