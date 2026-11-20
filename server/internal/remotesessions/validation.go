// Storage for live-validation verdicts; the probe itself lives with the MCP runtime.

package remotesessions

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RemoteSessionRef names the exact grant a probe presented, so a verdict lands on that grant or not at all.
type RemoteSessionRef struct {
	// ID is the remote_sessions row.
	ID uuid.UUID

	// Subject is the session subject the grant belongs to.
	Subject urn.SessionSubject

	// ClientID is the remote_session_client the grant was minted through.
	ClientID uuid.UUID

	// UpdatedAt is the row's CAS token as read before the probe.
	UpdatedAt time.Time

	// ProjectID is the consent challenge's project; the client row must belong to it.
	ProjectID uuid.UUID

	// OrganizationID is the consent challenge's organization, binding an org-level client.
	OrganizationID string
}

// RemoteSessionValidation is one observed verdict on a stored credential.
type RemoteSessionValidation struct {
	// Status is the observed verdict.
	Status ValidationOutcome

	// Reason is the Speakeasy-authored explanation of a non-valid status; empty when valid.
	Reason string

	// At is when the credential was presented.
	At time.Time
}

// RecordRemoteSessionValidation stores a verdict; false means the grant changed or vanished since ref was read,
// or an unknown lost the race to a valid or inactive already stored.
func (m *ChallengeManager) RecordRemoteSessionValidation(ctx context.Context, ref RemoteSessionRef, verdict RemoteSessionValidation) (bool, error) {
	switch verdict.Status {
	case ValidationOutcomeValid, ValidationOutcomeRejectedByMember, ValidationOutcomeInactive, ValidationOutcomeUnknown:
	default:
		return false, fmt.Errorf("record remote session validation: %q is not a probe verdict", verdict.Status)
	}
	rows, err := remotesessions_repo.New(m.db).SetRemoteSessionValidation(ctx, remotesessions_repo.SetRemoteSessionValidationParams{
		LastValidatedAt:       pgtype.Timestamptz{Time: verdict.At, Valid: true, InfinityModifier: pgtype.Finite},
		ValidationStatus:      string(verdict.Status),
		ValidationReason:      conv.ToPGTextEmpty(verdict.Reason),
		ID:                    ref.ID,
		SubjectUrn:            ref.Subject,
		RemoteSessionClientID: ref.ClientID,
		ExpectedUpdatedAt:     pgtype.Timestamptz{Time: ref.UpdatedAt, Valid: true, InfinityModifier: pgtype.Finite},
		ProjectID:             ref.ProjectID,
		OrganizationID:        ref.OrganizationID,
	})
	if err != nil {
		return false, fmt.Errorf("record remote session validation: %w", err)
	}
	return rows > 0, nil
}

// OpenRecheckToken reads an already scoped, exact unrenewable grant snapshot.
// A scheduled probe must never switch to another row or refresh a credential.
func (m *ChallengeManager) OpenRecheckToken(ctx context.Context, sess remotesessions_repo.RemoteSession) (UpstreamToken, error) {
	now := time.Now()
	if !authorizationUsable(sess, now) || (sess.AccessExpiresAt.Valid && !sess.AccessExpiresAt.Time.After(now)) ||
		hasRefreshToken(sess) || sess.RefreshExpiresAt.Valid {
		return UpstreamToken{}, ErrNoValidToken
	}
	token, err := openAccessToken(ctx, m.tokens, sess)
	if err != nil {
		return UpstreamToken{}, fmt.Errorf("open exact grant for re-check: %w", err)
	}
	if token == "" {
		return UpstreamToken{}, ErrNoValidToken
	}
	return UpstreamToken{ //nolint:exhaustruct_v5 // an exact subject token is not renewable here
		Token: token, Resource: conv.FromPGTextOrEmpty[string](sess.Resource), CredentialOwner: CredentialOwnerSubject,
		RemoteSessionClientID: sess.RemoteSessionClientID, RemoteSessionID: sess.ID,
		RemoteSessionUpdatedAt: sess.UpdatedAt.Time, RemoteSessionResolvedFromUpdatedAt: sess.UpdatedAt.Time,
	}, nil
}
