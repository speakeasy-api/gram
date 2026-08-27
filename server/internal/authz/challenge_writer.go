package authz

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

const (
	meterChallengeCHWriterSkipped  = "gram.authz_ch_writer.challenges_skipped"
	meterChallengeCHWriterInserted = "gram.authz_ch_writer.challenges_inserted"
)

// ChallengeInserter writes a batch of challenge rows to ClickHouse.
// *authzrepo.Queries satisfies it; tests supply a fake.
type ChallengeInserter interface {
	InsertChallenges(ctx context.Context, rows []authzrepo.ChallengeRow) error
}

// ChallengeCHWriter consumes authz challenge events from Pub/Sub and persists
// them to ClickHouse in batches. Invalid messages are poison records: they are
// logged and acknowledged, while ClickHouse failures are returned so the batch
// is redelivered.
type ChallengeCHWriter struct {
	logger             *slog.Logger
	inserter           ChallengeInserter
	challengesSkipped  metric.Int64Counter
	challengesInserted metric.Int64Counter
}

const (
	maxChallengeTraceIDBytes = 32
	maxChallengeSpanIDBytes  = 16
)

func NewChallengeCHWriter(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	inserter ChallengeInserter,
) *ChallengeCHWriter {
	logger = logger.With(attr.SlogComponent("authz-challenge-ch-writer"))
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/authz")
	challengesSkipped, err := meter.Int64Counter(
		meterChallengeCHWriterSkipped,
		metric.WithDescription("Authz challenge messages dropped by the ClickHouse writer as unprocessable"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterChallengeCHWriterSkipped), attr.SlogError(err))
	}
	challengesInserted, err := meter.Int64Counter(
		meterChallengeCHWriterInserted,
		metric.WithDescription("Authz challenge rows the ClickHouse writer attempted to insert"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterChallengeCHWriterInserted), attr.SlogError(err))
	}

	return &ChallengeCHWriter{
		logger:             logger,
		inserter:           inserter,
		challengesSkipped:  challengesSkipped,
		challengesInserted: challengesInserted,
	}
}

var _ streams.BatchHandler[*authzv1.Challenge] = (*ChallengeCHWriter)(nil)

func (w *ChallengeCHWriter) HandleBatch(ctx context.Context, messages []*authzv1.Challenge, _ []gcp.MessageMetadata) error {
	rows := make([]authzrepo.ChallengeRow, 0, len(messages))
	for _, message := range messages {
		row, err := challengeRowFromMessage(message)
		if err != nil {
			w.logger.ErrorContext(ctx, "skipping unprocessable authz challenge message",
				attr.SlogError(err),
				attr.SlogValueString(message.GetId()),
			)
			if w.challengesSkipped != nil {
				w.challengesSkipped.Add(ctx, 1)
			}
			continue
		}
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		return nil
	}

	err := w.inserter.InsertChallenges(ctx, rows)
	if w.challengesInserted != nil {
		w.challengesInserted.Add(ctx, int64(len(rows)), metric.WithAttributes(attr.Outcome(o11y.OutcomeFromError(err))))
	}
	if err != nil {
		return fmt.Errorf("insert authz challenges: %w", err)
	}

	return nil
}

func challengeRowFromMessage(message *authzv1.Challenge) (authzrepo.ChallengeRow, error) {
	if message == nil {
		return authzrepo.ChallengeRow{}, fmt.Errorf("message is nil")
	}
	if _, err := uuid.Parse(message.GetId()); err != nil {
		return authzrepo.ChallengeRow{}, fmt.Errorf("parse id: %w", err)
	}
	if len(message.GetTraceId()) > maxChallengeTraceIDBytes {
		return authzrepo.ChallengeRow{}, fmt.Errorf("trace id exceeds %d bytes", maxChallengeTraceIDBytes)
	}
	if len(message.GetSpanId()) > maxChallengeSpanIDBytes {
		return authzrepo.ChallengeRow{}, fmt.Errorf("span id exceeds %d bytes", maxChallengeSpanIDBytes)
	}
	timestamp, err := time.Parse(time.RFC3339Nano, message.GetTimestamp())
	if err != nil {
		return authzrepo.ChallengeRow{}, fmt.Errorf("parse timestamp: %w", err)
	}

	requestedChecks := make([]authzrepo.RequestedCheck, 0, len(message.GetRequestedChecks()))
	for _, check := range message.GetRequestedChecks() {
		requestedChecks = append(requestedChecks, authzrepo.RequestedCheck{
			Scope:        check.GetScope(),
			ResourceKind: check.GetResourceKind(),
			ResourceID:   check.GetResourceId(),
			Selector:     check.GetSelector(),
		})
	}

	matchedGrants := make([]authzrepo.MatchedGrant, 0, len(message.GetMatchedGrants()))
	for _, grant := range message.GetMatchedGrants() {
		matchedGrants = append(matchedGrants, authzrepo.MatchedGrant{
			PrincipalURN:         grant.GetPrincipalUrn(),
			Scope:                grant.GetScope(),
			Selector:             grant.GetSelector(),
			MatchedViaCheckScope: grant.GetMatchedViaCheckScope(),
		})
	}

	return authzrepo.ChallengeRow{
		ID:                   message.GetId(),
		Timestamp:            timestamp.UTC(),
		OrganizationID:       message.GetOrganizationId(),
		ProjectID:            message.GetProjectId(),
		TraceID:              message.GetTraceId(),
		SpanID:               message.GetSpanId(),
		RequestID:            conv.PtrEmpty(message.GetRequestId()),
		PrincipalURN:         message.GetPrincipalUrn(),
		PrincipalType:        authzrepo.PrincipalType(message.GetPrincipalType()),
		UserID:               conv.PtrEmpty(message.GetUserId()),
		UserExternalID:       conv.PtrEmpty(message.GetUserExternalId()),
		UserEmail:            conv.PtrEmpty(message.GetUserEmail()),
		APIKeyID:             conv.PtrEmpty(message.GetApiKeyId()),
		SessionID:            conv.PtrEmpty(message.GetSessionId()),
		RoleSlugs:            message.GetRoleSlugs(),
		Operation:            authzrepo.Operation(message.GetOperation()),
		Outcome:              authzrepo.Outcome(message.GetOutcome()),
		Reason:               authzrepo.Reason(message.GetReason()),
		Scope:                message.GetScope(),
		ResourceKind:         message.GetResourceKind(),
		ResourceID:           message.GetResourceId(),
		Selector:             message.GetSelector(),
		ExpandedScopes:       message.GetExpandedScopes(),
		RequestedChecks:      requestedChecks,
		MatchedGrants:        matchedGrants,
		EvaluatedGrantCount:  message.GetEvaluatedGrantCount(),
		FilterCandidateCount: message.GetFilterCandidateCount(),
		FilterAllowedCount:   message.GetFilterAllowedCount(),
	}, nil
}
