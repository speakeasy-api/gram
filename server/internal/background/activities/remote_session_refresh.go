package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const remoteSessionRefreshKeepalive = 24 * time.Hour

type ClaimDueRemoteSessionRefreshCandidatesInput struct {
	Limit int32
}

type RemoteSessionRefreshCandidate struct {
	SessionID      uuid.UUID
	OrganizationID string
	ProviderKey    string
}

type RefreshRemoteSessionInput struct {
	SessionID      uuid.UUID
	OrganizationID string
}

type RefreshRemoteSessionResult struct {
	RateLimited bool
}

type RemoteSessionRefresh struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	refresher *remotesessions.RefreshService
}

func NewRemoteSessionRefresh(logger *slog.Logger, db *pgxpool.Pool, refresher *remotesessions.RefreshService) *RemoteSessionRefresh {
	return &RemoteSessionRefresh{
		logger:    logger.With(attr.SlogComponent("remote-session-refresh")),
		db:        db,
		refresher: refresher,
	}
}

func (r *RemoteSessionRefresh) ClaimDueCandidates(
	ctx context.Context,
	input ClaimDueRemoteSessionRefreshCandidatesInput,
) ([]RemoteSessionRefreshCandidate, error) {
	now := time.Now()
	rows, err := remotesessions_repo.New(r.db).ClaimDueRemoteSessionRefreshCandidates(
		ctx,
		remotesessions_repo.ClaimDueRemoteSessionRefreshCandidatesParams{
			NowTs:           conv.ToPGTimestamptz(now),
			KeepaliveCutoff: conv.ToPGTimestamptz(now.Add(-remoteSessionRefreshKeepalive)),
			AttemptCutoff:   conv.ToPGTimestamptz(now.Add(-remoteSessionRefreshKeepalive)),
			LimitValue:      input.Limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("claim due remote session refresh candidates: %w", err)
	}

	candidates := make([]RemoteSessionRefreshCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, RemoteSessionRefreshCandidate{
			SessionID:      row.ID,
			OrganizationID: row.OrganizationID,
			ProviderKey:    remoteSessionRefreshProviderKey(row.TokenEndpoint),
		})
	}
	return candidates, nil
}

func remoteSessionRefreshProviderKey(endpoint pgtype.Text) string {
	if !endpoint.Valid || endpoint.String == "" {
		return ""
	}

	parsed, err := url.Parse(endpoint.String)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme + "://" + parsed.Host)
}

// Do re-checks that a claimed session is still due immediately before using
// its refresh grant. Post-attempt failures become outcomes rather than
// Temporal errors because retrying an ambiguous token POST can replay a
// rotating refresh token.
func (r *RemoteSessionRefresh) Do(ctx context.Context, input RefreshRemoteSessionInput) (RefreshRemoteSessionResult, error) {
	logger := r.logger.With(
		attr.SlogSessionID(input.SessionID.String()),
		attr.SlogOrganizationID(input.OrganizationID),
	)
	now := time.Now()
	candidate, err := remotesessions_repo.New(r.db).GetDueRemoteSessionRefreshCandidate(
		ctx,
		remotesessions_repo.GetDueRemoteSessionRefreshCandidateParams{
			ID:              input.SessionID,
			OrganizationID:  input.OrganizationID,
			NowTs:           conv.ToPGTimestamptz(now),
			KeepaliveCutoff: conv.ToPGTimestamptz(now.Add(-remoteSessionRefreshKeepalive)),
		},
	)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return RefreshRemoteSessionResult{RateLimited: false}, nil
	case err != nil:
		return RefreshRemoteSessionResult{}, fmt.Errorf("load due remote session refresh candidate: %w", err)
	}
	sess := candidate.RemoteSession

	result, err := r.refresher.RefreshNow(ctx, sess, "", remotesessionmetrics.RefreshTriggerScheduled)
	if err != nil {
		// The issuer URL and outcome are the ones the upstream-refresh metric
		// recorded, so these lines join to its series; the user id is what
		// lets "how many users are affected" be answered, since a client id is
		// one row per provider connection, not per user.
		args := []any{
			attr.SlogRemoteSessionClientID(sess.RemoteSessionClientID.String()),
			attr.SlogUserSessionIssuerID(sess.UserSessionIssuerID.String()),
		}
		var failure *remotesessions.RefreshError
		if errors.As(err, &failure) {
			args = append(args, attr.SlogOAuthIssuer(failure.IssuerURL), attr.SlogOutcome(string(failure.Outcome)))
		}
		if sess.SubjectUrn.Kind == urn.SessionSubjectKindUser {
			args = append(args, attr.SlogUserID(sess.SubjectUrn.ID))
		}
		if remotesessions.IsTokenRefreshRateLimited(err) {
			logger.WarnContext(ctx, "scheduled remote session refresh rate limited", args...)
			return RefreshRemoteSessionResult{RateLimited: true}, nil
		}
		logger.WarnContext(ctx, "scheduled remote session refresh failed", append(args, attr.SlogError(err))...)
		return RefreshRemoteSessionResult{RateLimited: false}, nil
	}

	logger.DebugContext(ctx, "scheduled remote session refresh completed",
		attr.SlogRemoteSessionClientID(sess.RemoteSessionClientID.String()),
		attr.SlogOutcome(string(result.Outcome)),
	)
	return RefreshRemoteSessionResult{RateLimited: false}, nil
}
