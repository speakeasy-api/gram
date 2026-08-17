package mv

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

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

// UpstreamKey identifies the (subject, issuer) pair that joins a user_session
// to the remote_sessions Gram holds on that subject's behalf. Both tables carry
// the pair, so it is the whole join — the inbound and outbound legs of one
// brokered connection meet here and nowhere else.
type UpstreamKey struct {
	SubjectURN          string
	UserSessionIssuerID uuid.UUID
}

// BuildUserSessionUpstreamIndex groups upstream rows by the pair they belong
// to, so a page of sessions can be built with one pass rather than a scan per
// session.
func BuildUserSessionUpstreamIndex(rows []repo.ListRemoteSessionUpstreamsForSubjectsRow) map[UpstreamKey][]*types.UserSessionUpstream {
	index := make(map[UpstreamKey][]*types.UserSessionUpstream, len(rows))
	for _, row := range rows {
		key := UpstreamKey{
			SubjectURN:          row.SubjectUrn.String(),
			UserSessionIssuerID: row.UserSessionIssuerID,
		}
		index[key] = append(index[key], buildUserSessionUpstreamView(row))
	}
	return index
}

func buildUserSessionUpstreamView(row repo.ListRemoteSessionUpstreamsForSubjectsRow) *types.UserSessionUpstream {
	return &types.UserSessionUpstream{
		RemoteSessionID:        row.ID.String(),
		RemoteSessionClientID:  row.RemoteSessionClientID.String(),
		RemoteSessionIssuerID:  row.RemoteSessionIssuerID.String(),
		IssuerSlug:             row.IssuerSlug,
		AccessExpiresAt:        formatOptionalTime(row.AccessExpiresAt),
		RefreshExpiresAt:       formatOptionalTime(row.RefreshExpiresAt),
		AuthorizationExpiresAt: formatOptionalTime(row.AuthorizationExpiresAt),
		HasRefreshToken:        row.HasRefreshToken,
		AutoRefresh:            row.AutoRefresh,
		LastUsedAt:             formatOptionalTime(row.LastUsedAt),
		Scopes:                 row.Scopes,
	}
}

// formatOptionalTime distinguishes "no value" from the zero time: an absent
// expiry means the upstream issued a non-expiring token, which is not the same
// as one that expired at the epoch.
func formatOptionalTime(ts pgtype.Timestamptz) *string {
	if !ts.Valid {
		return nil
	}
	s := ts.Time.Format(time.RFC3339)
	return &s
}

func BuildUserSessionView(row repo.ListUserSessionsByProjectIDRow, upstreams []*types.UserSessionUpstream) *types.UserSession {
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
		LastUsedAt:      formatOptionalTime(row.LastUsedAt),
		// Never nil: the field is required, and a session with no upstream is a
		// meaningful state (it reaches only Gram-native tools) that the client
		// renders differently from an absent one.
		Upstreams: upstreams,
	}
}

func BuildUserSessionListView(rows []repo.ListUserSessionsByProjectIDRow, upstreams map[UpstreamKey][]*types.UserSessionUpstream) []*types.UserSession {
	out := make([]*types.UserSession, len(rows))
	for i, row := range rows {
		found := upstreams[UpstreamKey{
			SubjectURN:          row.SubjectUrn.String(),
			UserSessionIssuerID: row.UserSessionIssuerID,
		}]
		if found == nil {
			found = []*types.UserSessionUpstream{}
		}
		out[i] = BuildUserSessionView(row, found)
	}
	return out
}
