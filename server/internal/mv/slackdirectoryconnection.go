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
	} else if row.CredentialsEncrypted.Valid {
		status = "connected"
	}
	scopes := append([]string{}, row.GrantedScopes...)
	result := &gen.SlackDirectoryConnection{ID: row.ID.String(), WorkspaceID: row.SlackTeamID, WorkspaceName: row.SlackTeamName.String, Status: status, Generation: row.Generation.String(), GrantedScopes: scopes, LastErrorCode: conv.FromPGText[string](row.LastErrorCode), DisconnectedAt: nil, UpdatedAt: row.UpdatedAt.Time.Format(time.RFC3339)}
	if row.DisconnectedAt.Valid {
		result.DisconnectedAt = conv.PtrEmpty(row.DisconnectedAt.Time.Format(time.RFC3339))
	}
	return result
}
