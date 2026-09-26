package mv

import (
	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"time"
)

func BuildSlackDirectoryConnectionView(row repo.SlackDirectoryConnection) *gen.SlackDirectoryConnection {
	status := "reconnect_required"
	if row.DisconnectedAt.Valid {
		status = "disconnected"
	} else if row.CredentialsEncrypted.Valid && row.Health == "connected" {
		status = "connected"
	}
	scopes := append([]string{}, row.GrantedScopes...)
	result := &gen.SlackDirectoryConnection{ID: row.ID.String(), WorkspaceID: row.SlackTeamID, WorkspaceName: row.SlackTeamName.String, Status: status, Generation: row.Generation.String(), GrantedScopes: scopes, LastErrorCode: conv.FromPGText[string](row.LastErrorCode), DisconnectedAt: nil, MemberCount: 0, DirectoryStatus: "never_synced", SyncStatus: "idle", SyncPhase: nil, SyncPages: nil, SyncMembers: nil, LastSyncStartedAt: nil, LastSyncFailedAt: nil, LastFullSyncSucceededAt: nil, UpdatedAt: row.UpdatedAt.Time.Format(time.RFC3339)}
	if row.DisconnectedAt.Valid {
		result.DisconnectedAt = conv.PtrEmpty(row.DisconnectedAt.Time.Format(time.RFC3339))
	}
	if row.LastSyncStartedAt.Valid {
		result.LastSyncStartedAt = conv.PtrEmpty(row.LastSyncStartedAt.Time.Format(time.RFC3339))
	}
	if row.LastSyncFailedAt.Valid {
		result.LastSyncFailedAt = conv.PtrEmpty(row.LastSyncFailedAt.Time.Format(time.RFC3339))
	}
	if row.LastFullSyncSucceededAt.Valid {
		result.LastFullSyncSucceededAt = conv.PtrEmpty(row.LastFullSyncSucceededAt.Time.Format(time.RFC3339))
		result.DirectoryStatus = "stale"
		if row.LastFullSyncGeneration.Valid && row.LastFullSyncGeneration.UUID == row.Generation && status == "connected" {
			result.DirectoryStatus = "current"
		}
	}
	return result
}
