// Package ssosignins records the SSO sign-ins that authorize MCP grants and
// judges whether a grant still traces to a current, revocable one before
// Speakeasy vouches for its user to another service.
package ssosignins

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/ssosignins/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// MaxAge bounds how long after its SSO sign-in a grant may still be vouched for.
const MaxAge = 7 * 24 * time.Hour

// MethodSSO is the WorkOS authentication method of an enterprise SSO sign-in,
// the only one that qualifies.
const MethodSSO = "SSO"

// RevokeReason records why a sign-in was revoked.
type RevokeReason string

const (
	RevokeReasonSignOut           RevokeReason = "sign_out"
	RevokeReasonSignOutEverywhere RevokeReason = "sign_out_everywhere"
	RevokeReasonAdmin             RevokeReason = "admin"
	RevokeReasonDeprovisioned     RevokeReason = "deprovisioned"
)

// Reason names why a grant is not eligible. Empty means eligible.
type Reason string

const (
	ReasonEligible             Reason = ""
	ReasonSessionNotFound      Reason = "session_not_found"
	ReasonNotLinked            Reason = "not_linked"
	ReasonNotUserSubject       Reason = "not_user_subject"
	ReasonSignInRevoked        Reason = "sign_in_revoked"
	ReasonNotSSO               Reason = "not_sso"
	ReasonExpired              Reason = "expired"
	ReasonOrganizationDisabled Reason = "organization_disabled"
	ReasonNotMember            Reason = "not_member"
	ReasonDeprovisioned        Reason = "deprovisioned"
	ReasonEmailDomain          Reason = "email_domain_not_verified"
)

// Identity is the verified identity of an eligible grant.
type Identity struct {
	UserID   string
	Email    string
	SignInID uuid.UUID
}

// Result is the outcome of CheckSignInIdentity: Identity is set only when
// Reason is ReasonEligible.
type Result struct {
	Reason   Reason
	Identity *Identity
}

// Eligible reports whether the grant may be vouched for.
func (r Result) Eligible() bool { return r.Reason == ReasonEligible && r.Identity != nil }

// RecordParams describes one WorkOS code exchange.
type RecordParams struct {
	OrganizationID       string
	UserID               string
	WorkOSSessionID      string
	AuthenticationMethod string
	AuthenticatedAt      time.Time
}

// Record upserts the sign-in for a WorkOS session. It records nothing and
// returns an invalid id when the exchange carries no session or organization.
func Record(ctx context.Context, db repo.DBTX, p RecordParams) (uuid.NullUUID, error) {
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if p.OrganizationID == "" || p.UserID == "" || p.WorkOSSessionID == "" {
		return none, nil
	}
	row, err := repo.New(db).UpsertSSOSignIn(ctx, repo.UpsertSSOSignInParams{
		OrganizationID:       p.OrganizationID,
		UserID:               p.UserID,
		WorkosSessionID:      p.WorkOSSessionID,
		AuthenticationMethod: p.AuthenticationMethod,
		AuthenticatedAt:      pgtype.Timestamptz{Time: p.AuthenticatedAt, InfinityModifier: pgtype.Finite, Valid: true},
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return none, errors.New("sso sign-in session belongs to another user")
	case err != nil:
		return none, fmt.Errorf("upsert sso sign-in: %w", err)
	case row.RevokedAt.Valid:
		return none, nil
	}
	return uuid.NullUUID{UUID: row.ID, Valid: true}, nil
}

// LiveSignInForSession returns the unrevoked sign-in recorded for a WorkOS
// session in an organization, or an invalid id when there is none.
func LiveSignInForSession(ctx context.Context, db repo.DBTX, organizationID, userID, workosSessionID string) (uuid.NullUUID, error) {
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if organizationID == "" || userID == "" || workosSessionID == "" {
		return none, nil
	}
	id, err := repo.New(db).GetLiveSSOSignInBySession(ctx, repo.GetLiveSSOSignInBySessionParams{
		OrganizationID:  organizationID,
		UserID:          userID,
		WorkosSessionID: workosSessionID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return none, nil
	case err != nil:
		return none, fmt.Errorf("get sso sign-in for session: %w", err)
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

// CheckSignInIdentity judges whether an MCP grant traces to a current SSO
// sign-in whose user Speakeasy may vouch for in organizationID. A grant without
// a recorded sign-in is ineligible, never an error.
func CheckSignInIdentity(ctx context.Context, db repo.DBTX, organizationID string, userSessionID uuid.UUID, now time.Time) (Result, error) {
	row, err := repo.New(db).GetSignInEligibility(ctx, repo.GetSignInEligibilityParams{
		OrganizationID: organizationID,
		UserSessionID:  userSessionID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Result{Reason: ReasonSessionNotFound, Identity: nil}, nil
	case err != nil:
		return Result{Reason: ReasonSessionNotFound, Identity: nil}, fmt.Errorf("get sign-in eligibility: %w", err)
	}
	return judge(row, now), nil
}

func judge(row repo.GetSignInEligibilityRow, now time.Time) Result {
	reject := func(reason Reason) Result { return Result{Reason: reason, Identity: nil} }
	if !row.SsoSignInID.Valid || !row.SignInID.Valid {
		return reject(ReasonNotLinked)
	}
	userID := row.SignInUserID.String
	if row.SubjectUrn.Kind != urn.SessionSubjectKindUser || row.SubjectUrn.ID != userID {
		return reject(ReasonNotUserSubject)
	}
	switch {
	case row.RevokedAt.Valid:
		return reject(ReasonSignInRevoked)
	case row.AuthenticationMethod.String != MethodSSO:
		return reject(ReasonNotSSO)
	case !row.AuthenticatedAt.Valid || now.Sub(row.AuthenticatedAt.Time) > MaxAge:
		return reject(ReasonExpired)
	case row.OrganizationDisabledAt.Valid:
		return reject(ReasonOrganizationDisabled)
	case !row.IsMember || !row.UserEmail.Valid:
		return reject(ReasonNotMember)
	case row.IsDeprovisioned:
		return reject(ReasonDeprovisioned)
	}
	email := conv.NormalizeEmail(row.UserEmail.String)
	if !emailOnDomain(email, row.VerifiedDomains) {
		return reject(ReasonEmailDomain)
	}
	return Result{Reason: ReasonEligible, Identity: &Identity{UserID: userID, Email: email, SignInID: row.SignInID.UUID}}
}

func emailOnDomain(email string, verified []string) bool {
	at := strings.LastIndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		return false
	}
	domain := workos.NormalizeDomain(email[at+1:])
	return slices.ContainsFunc(verified, func(d string) bool { return workos.NormalizeDomain(d) == domain })
}

// RevokeSession revokes the sign-ins recorded for one signed-out WorkOS
// session. Grants made from them stay valid for other paths but stop
// qualifying for vouching.
func RevokeSession(ctx context.Context, db repo.DBTX, organizationIDs []string, userID, workosSessionID string) (int64, error) {
	if len(organizationIDs) == 0 || userID == "" || workosSessionID == "" {
		return 0, nil
	}
	n, err := repo.New(db).RevokeSSOSignInsBySession(ctx, repo.RevokeSSOSignInsBySessionParams{
		RevokeReason:    conv.ToPGText(string(RevokeReasonSignOut)),
		OrganizationIds: organizationIDs,
		UserID:          userID,
		WorkosSessionID: workosSessionID,
	})
	if err != nil {
		return 0, fmt.Errorf("revoke sso sign-ins for session: %w", err)
	}
	return n, nil
}

// Revoked is what RevokeUser revoked.
type Revoked struct {
	// SignIns is how many sign-ins were newly revoked.
	SignIns int
	// WorkOSSessionIDs are the WorkOS sessions of the newly revoked sign-ins.
	WorkOSSessionIDs []string
	// Sessions are the soft-deleted grants. Callers push their JTIs into the
	// revocation cache after commit and audit each one.
	Sessions []RevokedSession
}

// RevokedSession is one grant RevokeUser soft-deleted.
type RevokedSession struct {
	ID             uuid.UUID
	OrganizationID string
	ProjectID      uuid.UUID
	Subject        urn.SessionSubject
	JTI            string
}

// RevokeUser revokes all of a user's sign-ins in the given organizations and
// soft-deletes every grant made from any of them.
func RevokeUser(ctx context.Context, db repo.DBTX, organizationIDs []string, userID string, reason RevokeReason) (Revoked, error) {
	var out Revoked
	if len(organizationIDs) == 0 || userID == "" {
		return out, nil
	}
	q := repo.New(db)
	signIns, err := q.RevokeUserSSOSignIns(ctx, repo.RevokeUserSSOSignInsParams{
		RevokeReason:    conv.ToPGText(string(reason)),
		OrganizationIds: organizationIDs,
		UserID:          userID,
	})
	if err != nil {
		return out, fmt.Errorf("revoke user sso sign-ins: %w", err)
	}
	out.SignIns = len(signIns)
	for _, s := range signIns {
		if !slices.Contains(out.WorkOSSessionIDs, s.WorkosSessionID) {
			out.WorkOSSessionIDs = append(out.WorkOSSessionIDs, s.WorkosSessionID)
		}
	}
	sessions, err := q.RevokeUserSessionsForUserSignIns(ctx, repo.RevokeUserSessionsForUserSignInsParams{
		OrganizationIds: organizationIDs,
		UserID:          userID,
	})
	if err != nil {
		return out, fmt.Errorf("revoke grants for user sso sign-ins: %w", err)
	}
	for _, s := range sessions {
		out.Sessions = append(out.Sessions, RevokedSession{
			ID:             s.ID,
			OrganizationID: s.OrganizationID.String,
			ProjectID:      s.ProjectID.UUID,
			Subject:        s.SubjectUrn,
			JTI:            s.Jti,
		})
	}
	return out, nil
}
