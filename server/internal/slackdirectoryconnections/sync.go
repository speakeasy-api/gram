package slackdirectoryconnections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// SyncInput identifies a requested snapshot without carrying credentials or profiles.
type SyncInput struct {
	OrganizationID string
	ConnectionID   uuid.UUID
	Generation     uuid.UUID
	ActorID        string
	// StartedAt is assigned once by the workflow, and survives activity retries.
	StartedAt time.Time
}

type SyncState struct {
	Status   string
	Progress SyncProgress
}

// SyncScheduler starts durable user actions and reports their current execution state.
type SyncScheduler interface {
	Start(context.Context, SyncInput) error
	State(context.Context, uuid.UUID, uuid.UUID) (SyncState, error)
}

type DirectorySync struct {
	db         *pgxpool.Pool
	encryption *encryption.Client
	provider   DirectoryProvider
	audit      *audit.Logger
	refresher  TokenRefresher
}

// NewDirectorySync takes an optional refresher; without one, expired tokens require reconnecting.
func NewDirectorySync(db *pgxpool.Pool, enc *encryption.Client, provider DirectoryProvider, auditLogger *audit.Logger, refresher TokenRefresher) *DirectorySync {
	return &DirectorySync{db: db, encryption: enc, provider: provider, audit: auditLogger, refresher: refresher}
}

// Refresh this long before expiry so a slow directory fetch cannot outlive the token.
const tokenRefreshMargin = 30 * time.Minute

func usableTokens(enc *encryption.Client, row repo.SlackDirectoryConnection) (*TokenBundle, error) {
	if row.DisconnectedAt.Valid || !row.CredentialsEncrypted.Valid || row.Health != "connected" {
		return nil, &SyncError{Code: "reconnect_required", Retryable: false, Reconnect: true, RetryAfter: 0}
	}
	plaintext, err := enc.Decrypt(row.CredentialsEncrypted.String)
	var tokens TokenBundle
	if err != nil || json.Unmarshal([]byte(plaintext), &tokens) != nil || tokens.Version != 1 || tokens.AccessToken == "" {
		return nil, &SyncError{Code: "credential_unavailable", Retryable: false, Reconnect: true, RetryAfter: 0}
	}
	// A rotating token past expiry stays usable while its refresh token can renew it.
	if tokens.ExpiresAt != nil && !time.Now().Before(*tokens.ExpiresAt) && tokens.RefreshToken == "" {
		return nil, &SyncError{Code: "authorization_expired", Retryable: false, Reconnect: true, RetryAfter: 0}
	}
	return &tokens, nil
}

// freshTokens refreshes a rotating token close to expiry and persists the new bundle
// before use, because Slack invalidates the old refresh token once it is exchanged.
func (s *DirectorySync) freshTokens(ctx context.Context, queries *repo.Queries, row repo.SlackDirectoryConnection) (*TokenBundle, error) {
	tokens, err := usableTokens(s.encryption, row)
	if err != nil {
		return nil, err
	}
	if tokens.ExpiresAt == nil || time.Until(*tokens.ExpiresAt) > tokenRefreshMargin {
		return tokens, nil
	}
	if s.refresher == nil || tokens.RefreshToken == "" {
		if time.Now().Before(*tokens.ExpiresAt) {
			return tokens, nil
		}
		return nil, &SyncError{Code: "authorization_expired", Retryable: false, Reconnect: true, RetryAfter: 0}
	}
	refreshed, err := s.refresher.Refresh(ctx, tokens.RefreshToken)
	if err != nil {
		if providerErr, ok := errors.AsType[*ProviderError](err); ok {
			switch providerErr.Code {
			case "transport", "http_status", "decode", "internal_error", "ratelimited", "rate_limited":
				return nil, &SyncError{Code: "refresh_unavailable", Retryable: true, Reconnect: false, RetryAfter: 0}
			}
		}
		return nil, &SyncError{Code: "authorization_expired", Retryable: false, Reconnect: true, RetryAfter: 0}
	}
	plaintext, err := json.Marshal(refreshed)
	if err != nil {
		return nil, fmt.Errorf("encode refreshed Slack credentials: %w", err)
	}
	ciphertext, err := s.encryption.Encrypt(plaintext)
	if err != nil {
		return nil, fmt.Errorf("encrypt refreshed Slack credentials: %w", err)
	}
	updated, err := queries.UpdateSlackDirectoryCredentials(ctx, repo.UpdateSlackDirectoryCredentialsParams{CredentialsEncrypted: conv.ToPGText(ciphertext), OrganizationID: row.OrganizationID, ID: row.ID, Generation: row.Generation})
	if err != nil {
		return nil, fmt.Errorf("store refreshed Slack credentials: %w", err)
	}
	if updated == 0 {
		// Disconnected or reauthorized meanwhile; the newer generation owns the credentials.
		return nil, errSyncSuperseded
	}
	return refreshed, nil
}

var errSyncSuperseded = errors.New("slack directory sync superseded")

// Run holds one database session lock across fetches, provider waits and publication.
// Temporal retries use the same lock. Publication uses that very session, so an
// attempt that loses its lock through a broken DB connection cannot publish.
func (s *DirectorySync) Run(ctx context.Context, input SyncInput, report func(SyncProgress)) error {
	if input.OrganizationID == constants.DemoOrganizationID {
		return &SyncError{Code: "demo_read_only", Retryable: false, Reconnect: false, RetryAfter: 0}
	}
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire Slack sync connection: %w", err)
	}
	defer conn.Release()
	queries := repo.New(conn)
	lockKey := "slack_directory_sync:" + input.ConnectionID.String()
	locked, err := queries.TryLockSlackDirectorySync(ctx, lockKey)
	if err != nil {
		return fmt.Errorf("lock Slack sync: %w", err)
	}
	if !locked {
		return &SyncError{Code: "sync_busy", Retryable: true, Reconnect: false, RetryAfter: 0}
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if released, err := queries.UnlockSlackDirectorySync(cleanup, lockKey); err != nil || !released {
			// Never put a session with an uncertain advisory lock back in the pool.
			o11y.NoLogDefer(func() error { return conn.Conn().Close(cleanup) })
		}
	}()
	row, err := queries.GetSlackDirectoryConnection(ctx, repo.GetSlackDirectoryConnectionParams{OrganizationID: input.OrganizationID, ID: input.ConnectionID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read Slack sync connection: %w", err)
	}
	started := input.StartedAt.UTC().Truncate(time.Microsecond)
	if row.Generation != input.Generation || row.DisconnectedAt.Valid {
		return nil
	}
	// An expired attempt can resume after its workflow closes. The persisted start
	// fences it from a newer run; a completed publication makes redelivery a no-op.
	if row.LastSyncStartedAt.Valid && row.LastSyncStartedAt.Time.After(started) {
		return nil
	}
	if row.LastFullSyncGeneration.Valid && row.LastFullSyncGeneration.UUID == input.Generation && row.LastFullSyncSucceededAt.Valid && !row.LastFullSyncSucceededAt.Time.Before(started) {
		return nil
	}
	if affected, err := queries.StartSlackDirectorySync(ctx, repo.StartSlackDirectorySyncParams{OrganizationID: input.OrganizationID, ID: input.ConnectionID, Generation: input.Generation, StartedAt: pgtype.Timestamptz{Time: started, Valid: true, InfinityModifier: pgtype.Finite}}); err != nil {
		return fmt.Errorf("record Slack sync start: %w", err)
	} else if affected == 0 {
		return nil
	}
	tokens, err := s.freshTokens(ctx, queries, row)
	if errors.Is(err, errSyncSuperseded) {
		return nil
	}
	if err != nil {
		return s.recordFailure(ctx, queries, input, started, err)
	}
	progress := SyncProgress{Phase: "fetching", Pages: 0, Members: 0, ExcludedExternal: 0, Bots: 0}
	if report == nil {
		report = func(SyncProgress) {}
	}
	members, err := s.provider.Fetch(ctx, tokens.AccessToken, row.SlackTeamID, func(p SyncProgress) { progress = p; report(p) })
	if err != nil {
		return s.recordFailure(ctx, queries, input, started, err)
	}
	progress.Phase = "publishing"
	report(progress)
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Slack directory publication: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)
	current, err := q.LockSlackDirectoryConnection(ctx, repo.LockSlackDirectoryConnectionParams{OrganizationID: input.OrganizationID, ID: input.ConnectionID})
	if err != nil {
		return fmt.Errorf("lock Slack directory publication: %w", err)
	}
	if current.Generation != input.Generation || current.DisconnectedAt.Valid || !current.LastSyncStartedAt.Valid || !current.LastSyncStartedAt.Time.Equal(started) {
		return nil
	}
	if _, err := usableTokens(s.encryption, current); err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return fmt.Errorf("rollback expired Slack sync: %w", rollbackErr)
		}
		return s.recordFailure(ctx, queries, input, started, err)
	}
	previousCount, err := q.CountSlackDirectorySnapshotMembers(ctx, repo.CountSlackDirectorySnapshotMembersParams{OrganizationID: input.OrganizationID, ID: input.ConnectionID})
	if err != nil {
		return fmt.Errorf("count previous Slack snapshot: %w", err)
	}
	published := pgtype.Timestamptz{Time: time.Now().UTC().Truncate(time.Microsecond), Valid: true, InfinityModifier: pgtype.Finite}
	for start := 0; start < len(members); start += 500 {
		batch := members[start:min(start+500, len(members))]
		params := repo.UpsertSlackDirectoryMembershipBatchParams{OrganizationID: input.OrganizationID, SlackTeamID: current.SlackTeamID, LastSeenAt: published,
			UserIds: make([]string, 0, len(batch)), DisplayNames: make([]string, 0, len(batch)), Emails: make([]string, 0, len(batch)), Statuses: make([]string, 0, len(batch)), MemberTypes: make([]string, 0, len(batch)), ProviderUpdatedAts: make([]pgtype.Timestamptz, 0, len(batch))}
		for _, member := range batch {
			updated := pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite}
			if member.UpdatedAt != nil {
				updated = pgtype.Timestamptz{Time: *member.UpdatedAt, Valid: true, InfinityModifier: pgtype.Finite}
			}
			params.UserIds = append(params.UserIds, member.UserID)
			params.DisplayNames = append(params.DisplayNames, member.DisplayName)
			params.Emails = append(params.Emails, member.Email)
			params.Statuses = append(params.Statuses, member.Status)
			params.MemberTypes = append(params.MemberTypes, member.MemberType)
			params.ProviderUpdatedAts = append(params.ProviderUpdatedAts, updated)
		}
		if err := q.UpsertSlackDirectoryMembershipBatch(ctx, params); err != nil {
			return fmt.Errorf("publish Slack directory members: %w", err)
		}
	}
	if err := q.MarkAbsentSlackDirectoryMembershipsUnknown(ctx, repo.MarkAbsentSlackDirectoryMembershipsUnknownParams{OrganizationID: input.OrganizationID, SlackTeamID: current.SlackTeamID, PublishedAt: published}); err != nil {
		return fmt.Errorf("retain absent Slack members: %w", err)
	}
	after, err := q.PublishSlackDirectorySync(ctx, repo.PublishSlackDirectorySyncParams{OrganizationID: input.OrganizationID, ID: input.ConnectionID, Generation: input.Generation, PublishedAt: published})
	if err != nil {
		return fmt.Errorf("record Slack snapshot: %w", err)
	}
	beforeView := mv.BuildSlackDirectoryConnectionView(current)
	beforeView.MemberCount = previousCount
	afterView := mv.BuildSlackDirectoryConnectionView(after)
	afterView.MemberCount = int64(len(members))
	if err := s.audit.LogSlackDirectoryConnectionSync(ctx, tx, audit.LogSlackDirectoryConnectionEvent{OrganizationID: input.OrganizationID, Actor: syncActor(input), ActorDisplayName: nil, ConnectionURN: urn.NewSlackDirectoryConnection(current.ID), ConnectionSnapshotBefore: beforeView, ConnectionSnapshotAfter: afterView}, audit.SlackDirectorySyncSummary{Observed: len(members), ExcludedExternal: progress.ExcludedExternal, Bots: progress.Bots}); err != nil {
		return fmt.Errorf("audit Slack snapshot: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Slack snapshot: %w", err)
	}
	return nil
}

func (s *DirectorySync) recordFailure(ctx context.Context, queries *repo.Queries, input SyncInput, started time.Time, failure error) error {
	code, reconnect := "sync_failed", false
	if providerErr, ok := errors.AsType[*SyncError](failure); ok {
		code, reconnect = providerErr.Code, providerErr.Reconnect
	}
	if affected, err := queries.FailSlackDirectorySync(ctx, repo.FailSlackDirectorySyncParams{OrganizationID: input.OrganizationID, ID: input.ConnectionID, Generation: input.Generation, StartedAt: pgtype.Timestamptz{Time: started, Valid: true, InfinityModifier: pgtype.Finite}, ErrorCode: conv.ToPGText(code), Reconnect: reconnect}); err != nil {
		return fmt.Errorf("record Slack sync failure: %w", err)
	} else if affected == 0 {
		return nil
	}
	return failure
}

// Scheduled syncs have no requesting user.
func syncActor(input SyncInput) urn.Principal {
	if input.ActorID == "" {
		return urn.NewSystemPrincipal("slack-directory-schedule")
	}
	return urn.NewPrincipal(urn.PrincipalTypeUser, input.ActorID)
}
