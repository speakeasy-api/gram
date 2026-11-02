package telemetry

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// toolCallCredentialLag bounds how long after a trace starts its gateway
// tool-call row is written. The row is stamped when the upstream answers, and
// gateway member calls time out well within an hour.
const toolCallCredentialLag = time.Hour

// toolCallCredentials reads, for the listed agent calls, the auth client each
// was signed in upstream with, and names the people whose connections served
// them. Best effort, like the authorizer labels: a failed read leaves the rows
// as they were recorded before credentials were tracked.
func (s *Service) toolCallCredentials(ctx context.Context, orgID, projectID string, rows []repo.ToolUsageTraceSummary) (map[string]repo.ToolCallCredential, map[string]string) {
	traceIDs := make([]string, 0, len(rows))
	var timeStart, timeEnd int64
	for _, row := range rows {
		if row.UserKind != repo.ToolUsageUserKindAgentID || row.TraceID == "" {
			continue
		}
		if len(traceIDs) == 0 || row.StartTimeUnixNano < timeStart {
			timeStart = row.StartTimeUnixNano
		}
		timeEnd = max(timeEnd, row.StartTimeUnixNano)
		traceIDs = append(traceIDs, row.TraceID)
	}
	if len(traceIDs) == 0 {
		return nil, nil
	}

	credentials, err := s.chRepo.ListToolCallCredentials(ctx, repo.ListToolCallCredentialsParams{
		GramProjectID: projectID,
		TraceIDs:      traceIDs,
		TimeStart:     timeStart,
		TimeEnd:       timeEnd + toolCallCredentialLag.Nanoseconds(),
	})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to read tool call credentials", attr.SlogError(err))
		return nil, nil
	}

	owners := make([]string, 0, len(credentials))
	for _, credential := range credentials {
		owners = append(owners, credential.ConnectionOwnerUserID)
	}
	return credentials, s.connectedUserNames(ctx, orgID, owners)
}

// connectedUserNames resolves user ids to a name for display: the person's
// display name, or their email when they have none. Scoped to the
// organization, and soft-deleted users resolve to nobody, as the authorizer
// labels do.
func (s *Service) connectedUserNames(ctx context.Context, orgID string, ids []string) map[string]string {
	ids = conv.DedupeNonEmpty(ids)
	if len(ids) == 0 || orgID == "" {
		return nil
	}

	users, err := usersRepo.New(s.db).GetConnectedUsersByIDs(ctx, usersRepo.GetConnectedUsersByIDsParams{
		Ids:            ids,
		OrganizationID: orgID,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to resolve tool call connection owners", attr.SlogError(err))
		return nil
	}
	names := make(map[string]string, len(users))
	for _, user := range users {
		if user.DeletedAt.Valid {
			continue
		}
		names[user.ID] = conv.Default(user.DisplayName, user.Email)
	}
	return names
}
