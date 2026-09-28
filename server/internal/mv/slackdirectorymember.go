package mv

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
)

func BuildSlackDirectoryMemberView(row repo.ListSlackDirectoryMembersRow) *gen.SlackDirectoryMember {
	result := &gen.SlackDirectoryMember{ID: row.ID.String(), ConnectionID: row.ConnectionID.String(), WorkspaceID: row.SlackTeamID, WorkspaceName: row.WorkspaceName.String, SlackUserID: row.SlackUserID, DisplayName: conv.FromPGText[string](row.DisplayName), Email: conv.FromPGText[string](row.Email), Status: row.Status, MemberType: row.MemberType, LastSeenAt: row.LastSeenAt.Time.Format(time.RFC3339), ObservedInLastSync: row.ObservedInLastSync,
		Mapping: nil, MappingRevision: row.MappingRevision, MappingStatus: "unmapped", ObservationToken: "", MappingConflictReason: conv.FromPGText[string](row.MappingConflictReason), MappingConflictDetectedAt: nil}
	if row.MappingConflictDetectedAt.Valid {
		result.MappingConflictDetectedAt = conv.PtrEmpty(row.MappingConflictDetectedAt.Time.Format(time.RFC3339Nano))
	}
	if row.MappingID != "" {
		result.Mapping = &gen.SlackIdentityMapping{ID: row.MappingID, UserID: row.MappedUserID, DisplayName: row.MappedDisplayName, Email: row.MappedEmail, PhotoURL: conv.FromPGText[string](row.MappedPhotoUrl), Active: row.MappedUserActive}
		result.MappingStatus = "mapped"
		if row.MappingConflictReason.Valid || !row.MappedUserActive {
			result.MappingStatus = "needs_review"
		}
	}
	// Bind confirmation to the displayed evidence, not sync timestamps: a no-op
	// publication must not invalidate an otherwise current review dialog.
	evidence, _ := json.Marshal([]any{result.ID, result.DisplayName, result.Email, result.Status, result.MemberType, result.ObservedInLastSync, result.MappingConflictReason, result.MappingConflictDetectedAt})
	digest := sha256.Sum256(evidence)
	result.ObservationToken = hex.EncodeToString(digest[:])
	return result
}
