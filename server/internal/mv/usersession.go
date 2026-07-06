package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func resolveSubject(row repo.ListUserSessionsByProjectIDRow) (subjectType string, displayName *string) {
	subjectType = string(row.SubjectUrn.Kind)
	switch row.SubjectUrn.Kind {
	case urn.SessionSubjectKindUser:
		if name := conv.FromPGText[string](row.UserDisplayName); name != nil && *name != "" {
			return subjectType, name
		}
		return subjectType, conv.FromPGText[string](row.UserEmail)
	case urn.SessionSubjectKindAPIKey:
		return subjectType, conv.FromPGText[string](row.ApiKeyName)
	default:
		return subjectType, nil
	}
}

func BuildUserSessionView(row repo.ListUserSessionsByProjectIDRow) *types.UserSession {
	subjectType, subjectName := resolveSubject(row)

	var revokedAt *string
	if row.Deleted && row.DeletedAt.Valid {
		s := row.DeletedAt.Time.Format(time.RFC3339)
		revokedAt = &s
	}

	return &types.UserSession{
		ID:                  row.ID.String(),
		UserSessionIssuerID: row.UserSessionIssuerID.String(),
		SubjectUrn:          row.SubjectUrn.String(),
		Jti:                 row.Jti,
		RefreshExpiresAt:    row.RefreshExpiresAt.Time.Format(time.RFC3339),
		ExpiresAt:           row.ExpiresAt.Time.Format(time.RFC3339),
		CreatedAt:           row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:           row.UpdatedAt.Time.Format(time.RFC3339),
		IssuerSlug:          row.IssuerSlug,
		ClientName:          conv.FromPGText[string](row.ClientName),
		SubjectType:         subjectType,
		SubjectDisplayName:  subjectName,
		RevokedAt:           revokedAt,
	}
}

func BuildUserSessionListView(rows []repo.ListUserSessionsByProjectIDRow) []*types.UserSession {
	out := make([]*types.UserSession, len(rows))
	for i, row := range rows {
		out[i] = BuildUserSessionView(row)
	}
	return out
}
