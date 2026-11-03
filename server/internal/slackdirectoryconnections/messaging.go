package slackdirectoryconnections

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
)

// MessagingWorkspace is a connected workspace whose bot token can post as the
// Speakeasy app right now. The token never leaves the process: callers use it
// for Slack Web API calls only.
type MessagingWorkspace struct {
	// ConnectionID is the organization's connection to the workspace.
	ConnectionID uuid.UUID

	// TeamID is the Slack workspace ID.
	TeamID string

	// Token is the workspace's bot token.
	Token string
}

// SlackAccount is a person's confirmed account in one connected workspace.
type SlackAccount struct {
	// TeamID is the Slack workspace ID.
	TeamID string

	// UserID is the person's Slack user ID in that workspace.
	UserID string
}

// minTokenLifetime is the least time a token must have left to be used when
// renewing it failed.
const minTokenLifetime = time.Minute

// MessagingWorkspaces returns the organization's connected workspaces that
// granted scope. A workspace that needs reconnecting is left out; the Slack
// workspaces page already tells admins to reconnect it. A workspace whose
// expiring token cannot be renewed right now is left out too, and reported in
// the error that comes back next to the workspaces that can be used, so one
// busy workspace does not stop the others.
func (s *DirectorySync) MessagingWorkspaces(ctx context.Context, organizationID, scope string) ([]MessagingWorkspace, error) {
	rows, err := repo.New(s.db).ListSlackDirectoryConnections(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Slack workspaces: %w", err)
	}
	workspaces := make([]MessagingWorkspace, 0, len(rows))
	var unavailable error
	for _, row := range rows {
		if !slices.Contains(row.GrantedScopes, scope) {
			continue
		}
		tokens, err := usableTokens(s.encryption, row)
		if err != nil {
			continue
		}
		if tokens.ExpiresAt != nil && time.Until(*tokens.ExpiresAt) <= tokenRefreshMargin {
			refreshed, err := s.refreshForMessaging(ctx, organizationID, row.ID)
			switch {
			case err == nil && refreshed == nil:
				continue
			case err == nil:
				tokens = refreshed
			case time.Until(*tokens.ExpiresAt) <= minTokenLifetime:
				unavailable = errors.Join(unavailable, err)
				continue
			default:
				// The current token still works; a later call renews it.
			}
		}
		workspaces = append(workspaces, MessagingWorkspace{ConnectionID: row.ID, TeamID: row.SlackTeamID, Token: tokens.AccessToken})
	}
	return workspaces, unavailable
}

// refreshForMessaging renews a rotating token under the directory sync lock, so
// it cannot race a sync that is refreshing the same token. It returns nil
// tokens when the workspace can no longer be used without reconnecting.
func (s *DirectorySync) refreshForMessaging(ctx context.Context, organizationID string, connectionID uuid.UUID) (*TokenBundle, error) {
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire Slack token refresh connection: %w", err)
	}
	defer conn.Release()
	queries := repo.New(conn)
	lockKey := syncLockKey(connectionID)
	locked, err := queries.TryLockSlackDirectorySync(ctx, lockKey)
	if err != nil {
		return nil, fmt.Errorf("lock Slack token refresh: %w", err)
	}
	if !locked {
		// A sync holds the lock and refreshes the token itself; try again later.
		return nil, &SyncError{Code: "sync_busy", Retryable: true, Reconnect: false, RetryAfter: 0}
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if released, err := queries.UnlockSlackDirectorySync(cleanup, lockKey); err != nil || !released {
			// Never put a session with an uncertain advisory lock back in the pool.
			o11y.NoLogDefer(func() error { return conn.Conn().Close(cleanup) })
		}
	}()
	row, err := queries.GetSlackDirectoryConnection(ctx, repo.GetSlackDirectoryConnectionParams{OrganizationID: organizationID, ID: connectionID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Slack workspace: %w", err)
	}
	tokens, err := s.freshTokens(ctx, queries, row)
	if errors.Is(err, errSyncSuperseded) {
		return nil, nil
	}
	if syncErr, ok := errors.AsType[*SyncError](err); ok && !syncErr.Retryable {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return tokens, nil
}

// syncLockKey names the advisory lock a workspace's directory sync and token
// renewal hold.
func syncLockKey(connectionID uuid.UUID) string {
	return "slack_directory_sync:" + connectionID.String()
}

// SlackAccounts lists a person's confirmed Slack accounts in workspaces that
// are still connected, newest first.
func SlackAccounts(ctx context.Context, db repo.DBTX, organizationID, userID string) ([]SlackAccount, error) {
	rows, err := repo.New(db).ListSlackAccountsForUser(ctx, repo.ListSlackAccountsForUserParams{OrganizationID: organizationID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("list Slack accounts: %w", err)
	}
	accounts := make([]SlackAccount, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, SlackAccount{TeamID: row.SlackTeamID, UserID: row.SlackUserID})
	}
	return accounts, nil
}
