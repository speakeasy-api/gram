package orgencryption

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
)

const (
	// runnerBatchSize is how many values one list query returns. The largest
	// organization holds a few thousand values, so a run takes a handful of
	// pages per column.
	runnerBatchSize = 200

	// unknownErrorCode is recorded when a platform-key decryption failure
	// carries no ENC-* code.
	unknownErrorCode = "unknown"
)

// RunResult reports one run for an organization and use case.
type RunResult struct {
	// Converted is how many values gained an organization-key copy.
	Converted int64

	// Counted reports whether the counts below were taken.
	Counted bool

	// Remaining is how many values still wait for an organization-key copy.
	Remaining int64

	// Unreadable is how many values are recorded as unreadable.
	Unreadable int64

	// Unattributed is how many values are stored without an organization.
	Unattributed int64

	// Verified is how many values verification opened and matched.
	Verified int64

	// Mismatched is how many values had copies that did not match, or a
	// platform-key copy that no longer decrypts beside an organization-key
	// copy.
	Mismatched int64

	// Unopenable is how many organization-key copies did not open.
	Unopenable int64

	// VerificationPassed reports whether a clean verification pass was
	// recorded.
	VerificationPassed bool
}

// Runner seals the values an organization stored before it was enrolled,
// verifies both copies of every value, and records the run on the transition
// row. It mints no keys: enroll provisions them.
type Runner struct {
	logger  *slog.Logger
	db      *pgxpool.Pool
	cipher  *Cipher
	metrics *runnerMetrics
}

// NewRunner builds a Runner over cipher, which must have organization
// encryption.
func NewRunner(logger *slog.Logger, db *pgxpool.Pool, cipher *Cipher, meterProvider metric.MeterProvider) *Runner {
	logger = logger.With(attr.SlogComponent("orgencryption.runner"))

	return &Runner{logger: logger, db: db, cipher: cipher, metrics: newRunnerMetrics(logger, meterProvider)}
}

// Run backfills and then verifies every column of an organization's use case,
// which must be in dual_write. A transient key problem stops the run and is
// recorded on the transition row; the next run resumes.
func (r *Runner) Run(ctx context.Context, organizationID string, useCase UseCase, stores []CopyStore) (result RunResult, err error) {
	result = RunResult{Converted: 0, Counted: false, Remaining: 0, Unreadable: 0, Unattributed: 0, Verified: 0, Mismatched: 0, Unopenable: 0, VerificationPassed: false}
	defer func() { r.metrics.record(ctx, organizationID, useCase, result, err) }()

	if err := requireStores(useCase, stores); err != nil {
		return result, err
	}

	d, err := r.cipher.Redecide(ctx, Decision{OrganizationID: organizationID, UseCase: useCase, State: "", Generation: 0, decidedAt: r.cipher.now()})
	if err != nil {
		return result, err
	}
	if d.State != TransitionStateDualWrite {
		return result, fmt.Errorf("%w: run in %s", ErrTransitionNotAllowed, d.State)
	}

	q := repo.New(r.db)
	logger := r.logger.With(attr.SlogOrganizationID(organizationID), attr.SlogOrgEncryptionUseCase(useCase))

	if err := q.RecordEncryptionTransitionBackfillStarted(ctx, repo.RecordEncryptionTransitionBackfillStartedParams{OrganizationID: organizationID, UseCase: string(useCase)}); err != nil {
		return result, fmt.Errorf("record backfill start: %w", err)
	}

	backfillErr := r.backfill(ctx, logger, q, &d, stores, &result)

	counts, countErr := countStores(ctx, organizationID, stores)
	if countErr == nil {
		result.Counted = true
		result.Remaining = counts.Waiting
		result.Unreadable = counts.Unreadable
		result.Unattributed = counts.Unattributed
	}

	runErr := errors.Join(backfillErr, countErr)
	if err := q.RecordEncryptionTransitionBackfillCompleted(ctx, repo.RecordEncryptionTransitionBackfillCompletedParams{
		ConvertedCount:  result.Converted,
		RemainingCount:  result.Remaining,
		UnreadableCount: result.Unreadable,
		Error:           errorSummary(runErr),
		OrganizationID:  organizationID,
		UseCase:         string(useCase),
	}); err != nil {
		return result, errors.Join(runErr, fmt.Errorf("record backfill completion: %w", err))
	}
	if runErr != nil {
		return result, runErr
	}

	if err := r.verify(ctx, logger, q, d, stores, &result); err != nil {
		return result, err
	}

	return result, nil
}

// backfill seals every waiting value. It refreshes the decision when it gets
// old and stops with ErrFenced if the transition state moved.
func (r *Runner) backfill(ctx context.Context, logger *slog.Logger, q *repo.Queries, d *Decision, stores []CopyStore, result *RunResult) error {
	for _, store := range stores {
		columns := store.Columns()
		after := ""

		for {
			values, err := store.ListWaiting(ctx, d.OrganizationID, after, runnerBatchSize)
			if err != nil {
				return fmt.Errorf("list waiting values in %s: %w", columns.PlatformColumn, err)
			}

			for _, value := range values {
				if err := r.refresh(ctx, d); err != nil {
					return err
				}
				if err := r.sealWaiting(ctx, logger, q, *d, store, value, result); err != nil {
					return err
				}
			}

			if len(values) < runnerBatchSize {
				break
			}
			after = values[len(values)-1].RowID
		}
	}

	return nil
}

// refresh replaces a decision close to MaxDecisionHold with a fresh one,
// failing with ErrFenced if the generation moved.
func (r *Runner) refresh(ctx context.Context, d *Decision) error {
	if r.cipher.now().Sub(d.decidedAt) < MaxDecisionHold/2 {
		return nil
	}

	fresh, err := r.cipher.Redecide(ctx, *d)
	if err != nil {
		return err
	}
	if fresh.Generation != d.Generation {
		return ErrFenced
	}
	*d = fresh

	return nil
}

func (r *Runner) sealWaiting(ctx context.Context, logger *slog.Logger, q *repo.Queries, d Decision, store CopyStore, value WaitingValue, result *RunResult) error {
	columns := store.Columns()
	unreadableKey := repo.DeleteEncryptionUnreadableValueParams{
		OrganizationID: d.OrganizationID,
		UseCase:        string(d.UseCase),
		SourceColumn:   columns.PlatformColumn,
		RowID:          value.RowID,
	}

	plaintext, err := r.cipher.platform.Decrypt(value.Platform)
	if err != nil {
		code, ok := encryption.ErrorCode(err)
		if !ok {
			code = unknownErrorCode
		}
		hash := sha256.Sum256([]byte(value.Platform))

		if err := q.UpsertEncryptionUnreadableValue(ctx, repo.UpsertEncryptionUnreadableValueParams{
			OrganizationID:           unreadableKey.OrganizationID,
			UseCase:                  unreadableKey.UseCase,
			SourceColumn:             unreadableKey.SourceColumn,
			RowID:                    unreadableKey.RowID,
			PlatformCiphertextSha256: hash[:],
			ErrorCode:                code,
		}); err != nil {
			return fmt.Errorf("record unreadable value: %w", err)
		}
		logger.WarnContext(ctx, "platform-key copy does not decrypt", attr.SlogOrgEncryptionErrorCode(code))

		return nil
	}

	if err := r.cipher.checkDecision(d, columns); err != nil {
		return err
	}

	org, err := r.cipher.sealOrg(ctx, d, columns, []byte(plaintext))
	if err != nil {
		return err
	}

	landed, err := store.SealWaiting(ctx, d.OrganizationID, d.Generation, value, org)
	if err != nil {
		return fmt.Errorf("seal waiting value in %s: %w", columns.OrgColumn, err)
	}
	if !landed {
		// A concurrent write replaced or erased the value, or the state
		// moved on. Either way the row no longer needs this copy.
		return nil
	}
	result.Converted++

	if err := q.DeleteEncryptionUnreadableValue(ctx, unreadableKey); err != nil {
		return fmt.Errorf("delete unreadable value record: %w", err)
	}

	return nil
}

// verify opens both copies of every sealed value and records a clean pass
// that started at the database time read before the first value.
func (r *Runner) verify(ctx context.Context, logger *slog.Logger, q *repo.Queries, d Decision, stores []CopyStore, result *RunResult) error {
	startedAt, err := q.GetEncryptionClock(ctx)
	if err != nil {
		return fmt.Errorf("read database clock: %w", err)
	}

	for _, store := range stores {
		columns := store.Columns()
		after := ""

		for {
			values, err := store.ListSealed(ctx, d.OrganizationID, after, runnerBatchSize)
			if err != nil {
				return fmt.Errorf("list sealed values in %s: %w", columns.OrgColumn, err)
			}

			for _, value := range values {
				if err := r.verifyValue(ctx, d, columns, value, result); err != nil {
					return err
				}
			}

			if len(values) < runnerBatchSize {
				break
			}
			after = values[len(values)-1].RowID
		}
	}

	if result.Mismatched > 0 || result.Unopenable > 0 {
		logger.ErrorContext(ctx, "organization encryption verification failed",
			attr.SlogOrgEncryptionMismatchedCount(result.Mismatched), attr.SlogOrgEncryptionUnopenableCount(result.Unopenable))
		return nil
	}

	if err := q.RecordEncryptionTransitionVerified(ctx, repo.RecordEncryptionTransitionVerifiedParams{
		VerifiedAt:     startedAt,
		OrganizationID: d.OrganizationID,
		UseCase:        string(d.UseCase),
		Generation:     d.Generation,
	}); err != nil {
		return fmt.Errorf("record verification: %w", err)
	}
	result.VerificationPassed = true

	return nil
}

func (r *Runner) verifyValue(ctx context.Context, d Decision, columns CopyColumns, value SealedValue, result *RunResult) error {
	org, err := r.cipher.openOrg(ctx, d, columns, value.Org)
	switch {
	case errors.Is(err, ErrUnreadable):
		result.Unopenable++
		return nil
	case err != nil:
		return err
	}

	platform, err := r.cipher.platform.Decrypt(value.Platform)
	if err != nil || !bytes.Equal([]byte(platform), org) {
		result.Mismatched++
		return nil
	}
	result.Verified++

	return nil
}

// errorSummary is the short error recorded on the transition row. Errors from
// this package and its column implementations never carry values or
// ciphertext.
func errorSummary(err error) pgtype.Text {
	if err == nil {
		return pgtype.Text{String: "", Valid: false}
	}

	message := err.Error()
	if len(message) > reasonMaxLen {
		message = message[:reasonMaxLen]
	}

	return conv.ToPGText(message)
}
