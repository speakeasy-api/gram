package authz

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// ChallengeCHWriter consumes authz challenge events from Pub/Sub and persists
// them to ClickHouse. Invalid messages are poison records: they are logged and
// acknowledged, while ClickHouse failures are returned for redelivery.
type ChallengeCHWriter struct {
	logger *slog.Logger
	conn   clickhouse.Conn
}

const (
	maxChallengeTraceIDBytes = 32
	maxChallengeSpanIDBytes  = 16
)

func NewChallengeCHWriter(
	logger *slog.Logger,
	conn clickhouse.Conn,
) *ChallengeCHWriter {
	return &ChallengeCHWriter{
		logger: logger.With(attr.SlogComponent("authz-challenge-ch-writer")),
		conn:   conn,
	}
}

func (w *ChallengeCHWriter) Handle(ctx context.Context, message *authzv1.Challenge, _ gcp.MessageMetadata) error {
	row, err := challengeRowFromMessage(message)
	if err != nil {
		w.logger.ErrorContext(ctx, "invalid authz challenge message",
			attr.SlogError(err),
			attr.SlogValueString(message.GetId()),
		)
		return nil
	}

	if err := authzrepo.New(w.conn).InsertChallenge(ctx, row); err != nil {
		return fmt.Errorf("insert authz challenge: %w", err)
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
