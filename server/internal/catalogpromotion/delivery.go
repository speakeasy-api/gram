package catalogpromotion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	delivery "github.com/speakeasy-api/gram/server/internal/thirdparty/catalogpromotion"
)

const (
	// DiscoveryRecordTimeout bounds one telemetry observation, including its database work.
	DiscoveryRecordTimeout = 10 * time.Second
	// DocsRecordTimeout bounds one documentation factory request.
	DocsRecordTimeout = 45 * time.Second
	// BatchTimeout bounds each global activity irrespective of fleet size.
	BatchTimeout = 3 * time.Minute
	// receiptTimeout bounds cleanup even when the activity has been canceled.
	receiptTimeout = 5 * time.Second
	// maxDocsFailures parks repeatedly failing requests for staff review.
	maxDocsFailures = 5
)

// DiscoveryBatch reports bounded progress; per-source failures have durable receipts.
type DiscoveryBatch struct {
	// More indicates due sources remain for the same weekly sweep.
	More bool
	// Failures counts observations unavailable in this batch.
	Failures int
}

// Discover processes at most one bounded batch for executions started before the weekly schedule.
// Due sources left by the page limit or batch deadline wait for a subsequent scheduled run.
func (s *Service) Discover(ctx context.Context) error {
	batch, err := s.DiscoverBatch(ctx)
	if err != nil {
		return err
	}
	if batch.Failures > 0 {
		return fmt.Errorf("refresh promotion discovery: %d candidates unavailable", batch.Failures)
	}
	return nil
}

// DiscoverBatch advances durable attempt ordering before observing each source, so failures cannot block subsequent batches.
func (s *Service) DiscoverBatch(ctx context.Context) (DiscoveryBatch, error) {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, BatchTimeout)
	defer cancel()
	q := repo.New(s.db)
	targets, err := q.DiscoveryTargets(ctx, repo.DiscoveryTargetsParams{Now: conv.ToPGTimestamptz(s.now()), ExcludedOrganizationID: constants.DemoOrganizationID})
	if err != nil {
		return DiscoveryBatch{}, fmt.Errorf("select promotion discovery batch: %w", err)
	}
	failed := 0
	for _, target := range targets {
		if ctx.Err() != nil {
			if parent.Err() != nil {
				return DiscoveryBatch{}, fmt.Errorf("promotion discovery canceled: %w", parent.Err())
			}
			break
		}
		recordCtx, recordCancel := context.WithTimeout(ctx, DiscoveryRecordTimeout)
		now := s.now()
		// Leave a day of headroom so processing time cannot skip the next weekly tick.
		row, claimErr := q.ClaimDiscovery(recordCtx, repo.ClaimDiscoveryParams{Now: conv.ToPGTimestamptz(now), NextAttemptAt: conv.ToPGTimestamptz(now.Add(6 * 24 * time.Hour)), OrganizationID: target.OrganizationID, ProjectID: target.ProjectID, McpServerID: target.McpServerID})
		if errors.Is(claimErr, pgx.ErrNoRows) {
			recordCancel()
			continue
		}
		if claimErr != nil {
			recordCancel()
			if ctx.Err() != nil && parent.Err() == nil {
				break
			}
			return DiscoveryBatch{}, fmt.Errorf("claim promotion discovery: %w", claimErr)
		}
		result, observeErr := s.GetCandidate(recordCtx, Target{OrganizationID: target.OrganizationID, ProjectID: target.ProjectID, MCPServerID: target.McpServerID})
		recordCancel()
		failureReason := discoveryFailureReason(observeErr, result.Reason)
		unavailable := failureReason == "telemetry_unavailable"
		saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), receiptTimeout)
		saveErr := q.FinishDiscovery(saveCtx, repo.FinishDiscoveryParams{Failed: unavailable, FailureReason: failureReason, ID: row.ID, AttemptedAt: conv.ToPGTimestamptz(now), ProjectID: row.ProjectID})
		saveCancel()
		if unavailable || saveErr != nil {
			failed++
		}
	}
	remainingCtx, remainingCancel := context.WithTimeout(parent, DiscoveryRecordTimeout)
	remaining, err := q.DiscoveryTargets(remainingCtx, repo.DiscoveryTargetsParams{Now: conv.ToPGTimestamptz(s.now()), ExcludedOrganizationID: constants.DemoOrganizationID})
	remainingCancel()
	if err != nil {
		return DiscoveryBatch{}, fmt.Errorf("check remaining promotion discovery: %w", err)
	}
	return DiscoveryBatch{More: len(remaining) > 0, Failures: failed}, nil
}

// Dispatch claims each due job before HTTP, then records its outcome independently
// of activity cancellation. Leases bound crash recovery and tokens fence late replies.
func (s *Service) Dispatch(ctx context.Context) error {
	if s.delivery == nil {
		return errors.New("documentation delivery dependency is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, BatchTimeout)
	defer cancel()
	rows, err := repo.New(s.db).PendingDeliveries(ctx, conv.ToPGTimestamptz(s.now()))
	if err != nil {
		return fmt.Errorf("select promotion deliveries: %w", err)
	}
	failed := 0
	for _, row := range rows {
		if ctx.Err() != nil {
			return fmt.Errorf("promotion delivery canceled: %w", ctx.Err())
		}
		if err := s.deliver(ctx, row); err != nil {
			failed++
		}
	}
	if ctx.Err() != nil {
		return fmt.Errorf("promotion delivery canceled: %w", ctx.Err())
	}
	if failed > 0 {
		return fmt.Errorf("dispatch catalogue promotion: %d requests need retry or attention", failed)
	}
	return nil
}

func (s *Service) deliver(ctx context.Context, target repo.RegistryPromotionCandidate) error {
	id := target.ID
	now := s.now()
	token := uuid.New()
	claimCtx, claimCancel := context.WithTimeout(ctx, receiptTimeout)
	row, err := repo.New(s.db).ClaimDocs(claimCtx, repo.ClaimDocsParams{Now: conv.ToPGTimestamptz(now), LeaseUntil: conv.ToPGTimestamptz(now.Add(DocsRecordTimeout + 30*time.Second)), LeaseToken: token, ID: id, ProjectID: target.ProjectID})
	claimCancel()
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("claim documentation request: %w", err)
	}
	effectCtx, effectCancel := context.WithTimeout(ctx, s.effectTimeout)
	var public publicSnapshot
	var issueURL string
	var effectErr error
	if row.DocsFailures > maxDocsFailures {
		effectErr = errors.New("documentation retry attempts exhausted")
	} else if err := json.Unmarshal(row.PublicMetadata, &public); err != nil {
		effectErr = &delivery.DeliveryError{Kind: delivery.ErrorPermanent, RetryAfter: 0, Code: "invalid_metadata"}
	} else if effectCtx.Err() != nil {
		effectErr = effectCtx.Err()
	} else {
		issueURL, effectErr = s.delivery.EnsureGuide(effectCtx, delivery.Guide{Slug: public.DocsSlug, Name: public.Title, Description: public.Description, RemoteURL: public.URL, DocumentationURL: public.DocumentationURL, AuthMode: public.AuthMode, AuthDiscovery: public.AuthDiscovery})
	}
	effectCancel()
	var result repo.FinishDocsParams
	result.ID = id
	result.LeaseToken = token
	result.ProjectID = row.ProjectID
	if effectErr == nil {
		result.DocsUrl = conv.ToPGText(issueURL)
		result.DocsQueuedAt = conv.ToPGTimestamptz(s.now())
	} else {
		kind, retryAfter := delivery.Classify(effectErr)
		result.Failures = min(row.DocsFailures, maxDocsFailures)
		code := "docs_transient"
		switch kind {
		case delivery.ErrorConfiguration:
			result.NeedsAttention = true
			code = "docs_not_configured"
		case delivery.ErrorPermanent:
			result.NeedsAttention = true
			code = "docs_permanent"
		case delivery.ErrorTransient, delivery.ErrorUnknown:
			if result.Failures >= maxDocsFailures {
				result.NeedsAttention = true
				code = "docs_attempts_exhausted"
			}
		}
		if !result.NeedsAttention || retryAfter > 0 {
			delay := time.Hour
			for i := int32(1); i < result.Failures; i++ {
				delay = min(delay*2, 24*time.Hour)
			}
			delay = max(delay, retryAfter)
			result.NextAttemptAt = conv.ToPGTimestamptz(s.now().Add(delay))
		}
		result.DeliveryError = conv.ToPGText(code)
	}
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), receiptTimeout)
	defer saveCancel()
	written, err := repo.New(s.db).FinishDocs(saveCtx, result)
	if err != nil {
		return fmt.Errorf("persist documentation outcome: %w", err)
	}
	if written == 0 {
		return ErrStale
	}
	if effectErr != nil {
		return errors.New("documentation request needs retry or attention")
	}
	return nil
}

// Status returns aggregate job health; LastDiscoveryAt records an attempted observation.
func (s *Service) Status(ctx context.Context) (JobStatus, error) {
	row, err := repo.New(s.db).JobStatus(ctx)
	if err != nil {
		return JobStatus{}, fmt.Errorf("read catalogue promotion job status: %w", err)
	}
	return JobStatus{PendingDocs: row.PendingDocs, NeedsAttention: row.NeedsAttention, OldestPendingAt: row.OldestPendingAt.Time, LastDiscoveryAt: row.LastDiscoveryAt.Time, DiscoveryFailures: row.DiscoveryFailures}, nil
}

// Source edits and deletion are expected races, not telemetry outages.
func discoveryFailureReason(err error, reason string) string {
	if errors.Is(err, ErrStale) || errors.Is(err, ErrNotFound) {
		return "source_changed"
	}
	if err != nil || reason == "telemetry_unavailable" {
		return "telemetry_unavailable"
	}
	return ""
}
