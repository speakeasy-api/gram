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
	"github.com/speakeasy-api/gram/server/internal/integrationcredentials"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
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
	db          *pgxpool.Pool
	credentials *integrationcredentials.Credentials
	provider    DirectoryProvider
	audit       *audit.Logger
	refresher   TokenRefresher
	// A sync holds a pooled connection for its whole Slack fetch, including
	// rate-limit waits, so this process runs at most a quarter of the pool at once.
	slots chan struct{}
}

// NewDirectorySync takes an optional refresher; without one, expired tokens require reconnecting.
func NewDirectorySync(db *pgxpool.Pool, credentials *integrationcredentials.Credentials, provider DirectoryProvider, auditLogger *audit.Logger, refresher TokenRefresher) *DirectorySync {
	return &DirectorySync{db: db, credentials: credentials, provider: provider, audit: auditLogger, refresher: refresher, slots: make(chan struct{}, max(1, db.Config().MaxConns/4))}
}

// Refresh this long before expiry so a slow directory fetch cannot outlive the token.
const tokenRefreshMargin = 30 * time.Minute

// errEncryptionUnavailable is an organization key that cannot be used right
// now. Reconnecting cannot help, so the workspace stays connected and the
// caller retries.
var errEncryptionUnavailable = &SyncError{Code: "encryption_unavailable", Retryable: true, Reconnect: false, RetryAfter: 0}

func usableTokens(ctx context.Context, credentials *integrationcredentials.Credentials, db integrationcredentials.DBTX, row repo.SlackDirectoryConnection) (*TokenBundle, error) {
	stored := integrationcredentials.Stored(row.CredentialsEncrypted, row.CredentialsOrgEncrypted)
	if row.DisconnectedAt.Valid || !integrationcredentials.Has(stored) || row.Health != "connected" {
		return nil, &SyncError{Code: "reconnect_required", Retryable: false, Reconnect: true, RetryAfter: 0}
	}
	plaintext, err := credentials.Open(ctx, db, row.OrganizationID, integrationcredentials.SlackDirectoryConnections, stored)
	if err != nil && !integrationcredentials.IsUnreadable(err) {
		return nil, errEncryptionUnavailable
	}
	var tokens TokenBundle
	if err != nil || json.Unmarshal(plaintext, &tokens) != nil {
		return nil, &SyncError{Code: "credential_unavailable", Retryable: false, Reconnect: true, RetryAfter: 0}
	}
	return checkTokens(&tokens)
}

func lockedTokensUsable(row repo.SlackDirectoryConnection, tokens *TokenBundle) (*TokenBundle, error) {
	if row.DisconnectedAt.Valid || !integrationcredentials.Has(integrationcredentials.Stored(row.CredentialsEncrypted, row.CredentialsOrgEncrypted)) || row.Health != "connected" {
		return nil, &SyncError{Code: "reconnect_required", Retryable: false, Reconnect: true, RetryAfter: 0}
	}
	return checkTokens(tokens)
}

func checkTokens(tokens *TokenBundle) (*TokenBundle, error) {
	if tokens.Version != 1 || tokens.AccessToken == "" {
		return nil, &SyncError{Code: "credential_unavailable", Retryable: false, Reconnect: true, RetryAfter: 0}
	}
	// A rotating token past expiry stays usable while its refresh token can renew it.
	if tokens.ExpiresAt != nil && !time.Now().Before(*tokens.ExpiresAt) && tokens.RefreshToken == "" {
		return nil, &SyncError{Code: "authorization_expired", Retryable: false, Reconnect: true, RetryAfter: 0}
	}
	return tokens, nil
}

// StoreTokens stores a workspace's token bundle, for the local development
// seed, which writes connections without the OAuth flow.
func StoreTokens(ctx context.Context, credentials *integrationcredentials.Credentials, db repo.DBTX, organizationID string, id, generation uuid.UUID, tokens *TokenBundle) error {
	return storeTokens(ctx, credentials, db, organizationID, id, generation, tokens)
}

// storeTokens seals the bundle under the organization's key and stores it on
// the connection's current generation. It returns errSyncSuperseded when the
// connection was disconnected or reauthorized meanwhile.
func storeTokens(ctx context.Context, credentials *integrationcredentials.Credentials, db repo.DBTX, organizationID string, id, generation uuid.UUID, tokens *TokenBundle) error {
	plaintext, err := json.Marshal(tokens)
	if err != nil {
		return fmt.Errorf("encode Slack credentials: %w", err)
	}
	queries := repo.New(db)
	err = credentials.Store(ctx, db, organizationID, integrationcredentials.SlackDirectoryConnections, plaintext, func(ctx context.Context, copies orgencryption.Copies, decided int64) (bool, error) {
		updated, err := queries.UpdateSlackDirectoryCredentials(ctx, repo.UpdateSlackDirectoryCredentialsParams{PlatformCopy: copies.Platform, OrgCopy: copies.Org, OrganizationID: organizationID, ID: id, Generation: generation, DecidedGeneration: decided})
		if err != nil {
			return false, fmt.Errorf("update Slack credentials: %w", err)
		}
		return updated > 0, nil
	})
	if errors.Is(err, integrationcredentials.ErrNotStored) {
		return errSyncSuperseded
	}
	if err != nil {
		return fmt.Errorf("store Slack credentials: %w", err)
	}
	return nil
}

// freshTokens refreshes a rotating token close to expiry and persists the new bundle
// before use, because Slack invalidates the old refresh token once it is exchanged.
func (s *DirectorySync) freshTokens(ctx context.Context, db repo.DBTX, row repo.SlackDirectoryConnection) (*TokenBundle, error) {
	tokens, err := usableTokens(ctx, s.credentials, db, row)
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
	// Slack invalidates the old refresh token on exchange, so check that the
	// new bundle can be stored before spending it.
	if err := s.credentials.RequireUsable(ctx, db, row.OrganizationID); err != nil {
		return nil, errEncryptionUnavailable
	}
	refreshed, err := s.refresher.Refresh(ctx, tokens.RefreshToken)
	if err != nil {
		if providerErr, ok := errors.AsType[*ProviderError](err); ok {
			switch providerErr.Code {
			case "ratelimited", "rate_limited":
				retryAfter := providerErr.RetryAfter
				if retryAfter <= 0 {
					retryAfter = time.Minute
				}
				return nil, &SyncError{Code: "rate_limited", Retryable: true, Reconnect: false, RetryAfter: retryAfter}
			case "transport", "http_status", "decode", "internal_error":
				return nil, &SyncError{Code: "refresh_unavailable", Retryable: true, Reconnect: false, RetryAfter: 0}
			}
		}
		return nil, &SyncError{Code: "authorization_expired", Retryable: false, Reconnect: true, RetryAfter: 0}
	}
	// Disconnected or reauthorized meanwhile means errSyncSuperseded: the newer generation owns the credentials.
	if err := storeTokens(ctx, s.credentials, db, row.OrganizationID, row.ID, row.Generation, refreshed); err != nil {
		if errors.Is(err, errSyncSuperseded) {
			return nil, err
		}
		return nil, fmt.Errorf("store refreshed Slack credentials: %w", err)
	}
	return refreshed, nil
}

var errSyncSuperseded = errors.New("slack directory sync superseded")

const minSyncBudget = 15 * time.Minute

// Run holds one database session lock across fetches, provider waits and publication.
// Temporal retries use the same lock. Publication uses that very session, so an
// attempt that loses its lock through a broken DB connection cannot publish.
func (s *DirectorySync) Run(ctx context.Context, input SyncInput, report func(SyncProgress)) error {
	if input.OrganizationID == constants.DemoOrganizationID {
		return &SyncError{Code: "demo_read_only", Retryable: false, Reconnect: false, RetryAfter: 0}
	}
	select {
	case s.slots <- struct{}{}:
	default:
		select {
		case s.slots <- struct{}{}:
		case <-ctx.Done():
			return fmt.Errorf("wait for a Slack sync slot: %w", ctx.Err())
		}
		// Waiting spent the attempt's budget; retry with a fresh one rather than start a fetch that cannot finish.
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < minSyncBudget {
			<-s.slots
			return &SyncError{Code: "sync_busy", Retryable: true, Reconnect: false, RetryAfter: 0}
		}
	}
	defer func() { <-s.slots }()
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire Slack sync connection: %w", err)
	}
	defer conn.Release()
	queries := repo.New(conn)
	lockKey := syncLockKey(input.ConnectionID)
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
	tokens, err := s.freshTokens(ctx, conn, row)
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
	// The generation still matches, so the credentials can only have been
	// refreshed since freshTokens read them. Check the decrypted bundle rather
	// than decrypting again under the row lock.
	if _, err := lockedTokensUsable(current, tokens); err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return fmt.Errorf("rollback expired Slack sync: %w", rollbackErr)
		}
		return s.recordFailure(ctx, queries, input, started, err)
	}
	previousCount, err := q.CountSlackDirectorySnapshotMembers(ctx, repo.CountSlackDirectorySnapshotMembersParams{OrganizationID: input.OrganizationID, ID: input.ConnectionID})
	if err != nil {
		return fmt.Errorf("count previous Slack snapshot: %w", err)
	}
	// Mapping mutations lock individual memberships. Lock all retained rows before
	// publication so subsequent statements see their committed mapping decisions.
	if _, err := q.LockSlackDirectoryMembershipsForPublication(ctx, repo.LockSlackDirectoryMembershipsForPublicationParams{OrganizationID: input.OrganizationID, SlackTeamID: current.SlackTeamID}); err != nil {
		return fmt.Errorf("lock Slack directory memberships: %w", err)
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
	if err := q.MarkAbsentSlackDirectoryMembershipsUnknown(ctx, repo.MarkAbsentSlackDirectoryMembershipsUnknownParams{OrganizationID: input.OrganizationID, SlackTeamID: current.SlackTeamID, PublishedAt: published, PreviousPublishedAt: current.LastFullSyncSucceededAt}); err != nil {
		return fmt.Errorf("retain absent Slack members: %w", err)
	}
	after, err := q.PublishSlackDirectorySync(ctx, repo.PublishSlackDirectorySyncParams{OrganizationID: input.OrganizationID, ID: input.ConnectionID, Generation: input.Generation, PublishedAt: published})
	if err != nil {
		return fmt.Errorf("record Slack snapshot: %w", err)
	}
	if err := autoMapByEmail(ctx, tx, s.audit, input.OrganizationID, current.SlackTeamID); err != nil {
		return err
	}
	beforeView := mv.BuildSlackDirectoryConnectionView(current)
	beforeView.MemberCount = previousCount
	afterView := mv.BuildSlackDirectoryConnectionView(after)
	afterView.MemberCount = int64(len(members))
	// Scheduled syncs are routine system work; auditing each would add 48 entries per workspace per day.
	if input.ActorID != "" {
		if err := s.audit.LogSlackDirectoryConnectionSync(ctx, tx, audit.LogSlackDirectoryConnectionEvent{OrganizationID: input.OrganizationID, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, input.ActorID), ActorDisplayName: nil, ConnectionURN: urn.NewSlackDirectoryConnection(current.ID), ConnectionSnapshotBefore: beforeView, ConnectionSnapshotAfter: afterView}, audit.SlackDirectorySyncSummary{Observed: len(members), ExcludedExternal: progress.ExcludedExternal, Bots: progress.Bots}); err != nil {
			return fmt.Errorf("audit Slack snapshot: %w", err)
		}
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
