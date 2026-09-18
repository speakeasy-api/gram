package mv

import (
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/clientcred"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func resolveSubject(row repo.ListUserSessionsByProjectIDRow, workload *types.UserSessionWorkload) (subjectType string, displayName *string) {
	subjectType = string(row.SubjectUrn.Kind)
	switch row.SubjectUrn.Kind {
	case urn.SessionSubjectKindUser:
		if name := conv.FromPGText[string](row.UserDisplayName); name != nil && *name != "" {
			return subjectType, name
		}
		return subjectType, conv.FromPGText[string](row.UserEmail)
	case urn.SessionSubjectKindAPIKey:
		return subjectType, conv.FromPGText[string](row.ApiKeyName)
	case urn.SessionSubjectKindWorkload:
		// The external subject is the legible half of the workload's identity;
		// the issuer that scopes it travels alongside in the workload view.
		if workload == nil {
			return subjectType, nil
		}
		return subjectType, &workload.ExternalSubject
	default:
		return subjectType, nil
	}
}

// WorkloadKey identifies a workload principal: the workload issuer that vouched
// for it and the subject that issuer asserted. The subject alone names nothing,
// since every issuer mints its own.
type WorkloadKey struct {
	WorkloadIssuerID uuid.UUID
	ExternalSubject  string
}

// WorkloadKeyForSession splits a workload session's subject into its key. It
// reports false for any other subject kind.
func WorkloadKeyForSession(subject urn.SessionSubject) (WorkloadKey, bool) {
	if subject.Kind != urn.SessionSubjectKindWorkload {
		return WorkloadKey{WorkloadIssuerID: uuid.Nil, ExternalSubject: ""}, false
	}
	issuerID, externalSubject, err := subject.Workload()
	if err != nil {
		return WorkloadKey{WorkloadIssuerID: uuid.Nil, ExternalSubject: ""}, false
	}
	return WorkloadKey{WorkloadIssuerID: issuerID, ExternalSubject: externalSubject}, true
}

// BuildUserSessionWorkloadIndex keys resolved workload labels by the workload
// they describe, so a page of sessions is labelled in one pass. Each workload
// carries the admissions that currently let it in.
func BuildUserSessionWorkloadIndex(rows []repo.ListWorkloadSessionLabelsRow, admissionRows []repo.ListWorkloadSessionAdmissionsRow) map[WorkloadKey]*types.UserSessionWorkload {
	admissions := make(map[WorkloadKey][]*types.UserSessionWorkloadAdmission, len(rows))
	for _, row := range admissionRows {
		key := WorkloadKey{WorkloadIssuerID: row.WorkloadIssuerID, ExternalSubject: row.Subject}
		admissions[key] = append(admissions[key], &types.UserSessionWorkloadAdmission{
			ID:   row.ID.String(),
			Tier: conv.Ternary(row.ProjectID.Valid, "project", "organization"),
			Name: conv.FromPGText[string](row.Name),
		})
	}

	index := make(map[WorkloadKey]*types.UserSessionWorkload, len(rows))
	for _, row := range rows {
		var agentID *string
		if row.AgentID.Valid {
			id := row.AgentID.UUID.String()
			agentID = &id
		}

		var agentStatus *string
		if row.AgentID.Valid {
			status := "active"
			switch {
			case row.AgentRevokedAt.Valid:
				status = "revoked"
			case row.AgentSuspendedAt.Valid:
				status = "suspended"
			}
			agentStatus = &status
		}

		key := WorkloadKey{WorkloadIssuerID: row.WorkloadIssuerID, ExternalSubject: row.Subject}
		index[key] = &types.UserSessionWorkload{
			WorkloadIssuerID:   row.WorkloadIssuerID.String(),
			ExternalSubject:    row.Subject,
			WorkloadIssuerName: conv.FromPGText[string](row.WorkloadIssuerName),
			WorkloadIssuerURL:  conv.FromPGText[string](row.WorkloadIssuerUrl),
			AgentID:            agentID,
			AgentName:          conv.FromPGText[string](row.AgentName),
			AgentStatus:        agentStatus,
			Admissions:         conv.DefaultSlice(admissions[key], []*types.UserSessionWorkloadAdmission{}),
		}
	}
	return index
}

// buildWorkloadView returns the resolved label for a workload session, falling
// back to the identity parsed from the subject when nothing resolved. A
// workload session always carries its issuer and subject, even when the issuer
// is no longer visible.
func buildWorkloadView(subject urn.SessionSubject, workloads map[WorkloadKey]*types.UserSessionWorkload) *types.UserSessionWorkload {
	key, ok := WorkloadKeyForSession(subject)
	if !ok {
		return nil
	}
	if found, ok := workloads[key]; ok {
		return found
	}
	return &types.UserSessionWorkload{
		WorkloadIssuerID:   key.WorkloadIssuerID.String(),
		ExternalSubject:    key.ExternalSubject,
		WorkloadIssuerName: nil,
		WorkloadIssuerURL:  nil,
		AgentID:            nil,
		AgentName:          nil,
		AgentStatus:        nil,
		Admissions:         []*types.UserSessionWorkloadAdmission{},
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

// The expiry fields stay nil rather than carrying a zero time: an absent expiry
// means the upstream issued a non-expiring token, which is not the same as one
// that expired at the epoch.
func buildUserSessionUpstreamView(row repo.ListRemoteSessionUpstreamsForSubjectsRow) *types.UserSessionUpstream {
	return &types.UserSessionUpstream{
		RemoteSessionID:        row.ID.String(),
		RemoteSessionClientID:  row.RemoteSessionClientID.String(),
		RemoteSessionIssuerID:  row.RemoteSessionIssuerID.String(),
		IssuerSlug:             row.IssuerSlug,
		AccessExpiresAt:        conv.PtrEmpty(conv.FromPGTimestamptz(row.AccessExpiresAt)),
		RefreshExpiresAt:       conv.PtrEmpty(conv.FromPGTimestamptz(row.RefreshExpiresAt)),
		AuthorizationExpiresAt: conv.PtrEmpty(conv.FromPGTimestamptz(row.AuthorizationExpiresAt)),
		HasRefreshToken:        row.HasRefreshToken,
		AutoRefresh:            row.AutoRefresh,
		LastUsedAt:             conv.PtrEmpty(conv.FromPGTimestamptz(row.LastUsedAt)),
		Scopes:                 row.Scopes,
	}
}

func BuildUserSessionView(row repo.ListUserSessionsByProjectIDRow, upstreams []*types.UserSessionUpstream, workloads map[WorkloadKey]*types.UserSessionWorkload) *types.UserSession {
	workload := buildWorkloadView(row.SubjectUrn, workloads)
	subjectType, subjectName := resolveSubject(row, workload)

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

	credentialKind, declaredAuthMethod := clientcred.ForBoundClient(
		row.UserSessionClientID.Valid,
		row.ClientTokenEndpointAuthMethod,
		row.ClientHasSecret,
	)

	return &types.UserSession{
		ID:                            row.ID.String(),
		UserSessionIssuerID:           row.UserSessionIssuerID.String(),
		SubjectUrn:                    row.SubjectUrn.String(),
		Jti:                           row.Jti,
		RefreshExpiresAt:              row.RefreshExpiresAt.Time.Format(time.RFC3339),
		ExpiresAt:                     row.ExpiresAt.Time.Format(time.RFC3339),
		CreatedAt:                     row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                     row.UpdatedAt.Time.Format(time.RFC3339),
		IssuerSlug:                    row.IssuerSlug,
		UserSessionClientID:           clientID,
		ClientName:                    conv.FromPGText[string](row.ClientName),
		ClientIDMetadataURI:           conv.FromPGText[string](row.ClientIDMetadataUri),
		ClientCredentialKind:          credentialKind,
		ClientTokenEndpointAuthMethod: declaredAuthMethod,
		SubjectType:                   subjectType,
		SubjectDisplayName:            subjectName,
		// Only a user subject resolves to a users row, so the join leaves this
		// NULL for API key and anonymous subjects.
		SubjectPhotoURL: conv.FromPGText[string](row.UserPhotoUrl),
		RevokedAt:       revokedAt,
		LastUsedAt:      conv.PtrEmpty(conv.FromPGTimestamptz(row.LastUsedAt)),
		// Never nil: the field is required, and a session with no upstream is a
		// meaningful state (it reaches only Gram-native tools) that the client
		// renders differently from an absent one.
		Upstreams: upstreams,
		Workload:  workload,
	}
}

func BuildUserSessionListView(rows []repo.ListUserSessionsByProjectIDRow, upstreams map[UpstreamKey][]*types.UserSessionUpstream, workloads map[WorkloadKey]*types.UserSessionWorkload) []*types.UserSession {
	out := make([]*types.UserSession, len(rows))
	for i, row := range rows {
		found := upstreams[UpstreamKey{
			SubjectURN:          row.SubjectUrn.String(),
			UserSessionIssuerID: row.UserSessionIssuerID,
		}]
		if found == nil {
			found = []*types.UserSessionUpstream{}
		}
		out[i] = BuildUserSessionView(row, found, workloads)
	}
	return out
}
