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

	// A session can be issued without a client (the API key and anonymous
	// paths mint one directly), so the column is nullable.
	var clientID *string
	if row.UserSessionClientID.Valid {
		s := row.UserSessionClientID.UUID.String()
		clientID = &s
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
		UserSessionClientID: clientID,
		ClientName:          conv.FromPGText[string](row.ClientName),
		ClientIDMetadataURI: conv.FromPGText[string](row.ClientIDMetadataUri),
		SubjectType:         subjectType,
		SubjectDisplayName:  subjectName,
		// Only a user subject resolves to a users row, so the join leaves this
		// NULL for API key and anonymous subjects.
		SubjectPhotoURL: conv.FromPGText[string](row.UserPhotoUrl),
		RevokedAt:       revokedAt,
	}
}

func BuildUserSessionListView(rows []repo.ListUserSessionsByProjectIDRow) []*types.UserSession {
	out := make([]*types.UserSession, len(rows))
	for i, row := range rows {
		out[i] = BuildUserSessionView(row)
	}
	return out
}
