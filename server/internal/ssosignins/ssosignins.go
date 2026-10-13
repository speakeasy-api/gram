// Package ssosignins records the SSO sign-ins that authorize MCP grants and
// judges whether a grant still traces to a current, revocable one.
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

// MaxAge is the default bound on how long after its SSO sign-in a grant may
// still be vouched for.
// TODO: prune revoked and expired sso_sign_ins rows (retention sweep follow-up).
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
	ReasonSessionExpired       Reason = "session_expired"
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

// Recorded is the sign-in a code exchange resolved to. A revoked row is still
// returned, so a grant made from it stays reachable by later revocations and
// is never vouched for.
type Recorded struct {
	ID      uuid.NullUUID
	Revoked bool
}

var errSignInOwnedByAnotherUser = errors.New("sso sign-in session belongs to another user")

// Record upserts the sign-in for a WorkOS session. It records nothing when the
// exchange carries no session, organization or user.
func Record(ctx context.Context, db repo.DBTX, p RecordParams) (Recorded, error) {
	none := Recorded{ID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Revoked: false}
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
		return none, errSignInOwnedByAnotherUser
	case err != nil:
		return none, fmt.Errorf("upsert sso sign-in: %w", err)
	}
	return Recorded{ID: uuid.NullUUID{UUID: row.ID, Valid: true}, Revoked: row.RevokedAt.Valid}, nil
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

// SignInForSession returns the sign-in recorded for a WorkOS session in an
// organization, revoked or not, or an invalid id when there is none.
func SignInForSession(ctx context.Context, db repo.DBTX, organizationID, userID, workosSessionID string) (uuid.NullUUID, error) {
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if organizationID == "" || userID == "" || workosSessionID == "" {
		return none, nil
	}
	id, err := repo.New(db).GetSSOSignInBySession(ctx, repo.GetSSOSignInBySessionParams{
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
// sign-in whose user Speakeasy may vouch for in organizationID. maxAge <= 0
// uses MaxAge. A grant without a recorded sign-in is ineligible, never an
// error.
func CheckSignInIdentity(ctx context.Context, db repo.DBTX, organizationID string, userSessionID uuid.UUID, now time.Time, maxAge time.Duration) (Result, error) {
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
	if maxAge <= 0 {
		maxAge = MaxAge
	}
	return judge(row, now, maxAge), nil
}

func judge(row repo.GetSignInEligibilityRow, now time.Time, maxAge time.Duration) Result {
	reject := func(reason Reason) Result { return Result{Reason: reason, Identity: nil} }
	if !row.SessionLive {
		return reject(ReasonSessionExpired)
	}
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
	case !row.AuthenticatedAt.Valid || now.Sub(row.AuthenticatedAt.Time) > maxAge:
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

// SignedOut is what RevokeSession revoked.
type SignedOut struct {
	// Organizations hold a sign-in this sign-out revoked.
	Organizations []string
	// DeliveryIDs are the durable deliveries it enqueued.
	DeliveryIDs []uuid.UUID
}

// RevokeSession revokes the sign-ins recorded for one signed-out WorkOS
// session and enqueues ending that WorkOS session. Grants made from them keep
// working but stop qualifying for vouching. Locks are taken in id order, like
// every other revocation.
func RevokeSession(ctx context.Context, db repo.DBTX, organizationIDs []string, userID, workosSessionID string) (SignedOut, error) {
	var out SignedOut
	if len(organizationIDs) == 0 || userID == "" || workosSessionID == "" {
		return out, nil
	}
	q := repo.New(db)
	if _, err := q.LockSSOSignInsBySession(ctx, repo.LockSSOSignInsBySessionParams{OrganizationIds: organizationIDs, UserID: userID, WorkosSessionID: workosSessionID}); err != nil {
		return out, fmt.Errorf("lock sso sign-ins for session: %w", err)
	}
	rows, err := q.RevokeSSOSignInsBySession(ctx, repo.RevokeSSOSignInsBySessionParams{
		RevokeReason:    conv.ToPGText(string(RevokeReasonSignOut)),
		OrganizationIds: organizationIDs,
		UserID:          userID,
		WorkosSessionID: workosSessionID,
	})
	if err != nil {
		return out, fmt.Errorf("revoke sso sign-ins for session: %w", err)
	}
	seen := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		if _, ok := seen[r.OrganizationID]; ok {
			continue
		}
		seen[r.OrganizationID] = struct{}{}
		out.Organizations = append(out.Organizations, r.OrganizationID)
		ids, err := enqueue(ctx, q, r.OrganizationID, DeliveryWorkOSSession, []string{workosSessionID})
		if err != nil {
			return out, err
		}
		out.DeliveryIDs = append(out.DeliveryIDs, ids...)
	}
	return out, nil
}

// SignInOrganizations returns every organization the user has a recorded
// sign-in in, including organizations they have since left.
func SignInOrganizations(ctx context.Context, db repo.DBTX, userID string) ([]string, error) {
	if userID == "" {
		return nil, nil
	}
	orgs, err := repo.New(db).ListSSOSignInOrganizationsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list sso sign-in organizations: %w", err)
	}
	return orgs, nil
}

// Revoked is what RevokeUser revoked.
type Revoked struct {
	// SignInsByOrganization counts newly revoked or upgraded sign-ins per
	// organization.
	SignInsByOrganization map[string]int
	// WorkOSSessionIDs are the WorkOS sessions of those sign-ins.
	WorkOSSessionIDs []string
	// Sessions are the soft-deleted grants.
	Sessions []RevokedSession
	// DeliveryIDs are the durable deliveries enqueued for the JTIs and WorkOS
	// sessions.
	DeliveryIDs []uuid.UUID
}

// SignIns is the total number of newly revoked sign-ins.
func (r Revoked) SignIns() int {
	total := 0
	for _, n := range r.SignInsByOrganization {
		total += n
	}
	return total
}

// SessionsInOrganization counts the soft-deleted grants in one organization.
func (r Revoked) SessionsInOrganization(organizationID string) int {
	n := 0
	for _, s := range r.Sessions {
		if s.OrganizationID == organizationID {
			n++
		}
	}
	return n
}

// RevokedSession is one grant RevokeUser soft-deleted.
type RevokedSession struct {
	ID             uuid.UUID
	OrganizationID string
	ProjectID      uuid.UUID
	Subject        urn.SessionSubject
	JTI            string
}

// RevokeUser revokes all of a user's sign-ins in the given organizations,
// including ones a single sign-out already revoked, fences out sign-ins still
// being recorded, soft-deletes every grant made from them and enqueues the
// JTI and WorkOS session deliveries. It locks the sign-ins first, in id order,
// matching grant issuance.
func RevokeUser(ctx context.Context, db repo.DBTX, organizationIDs []string, userID string, reason RevokeReason) (Revoked, error) {
	out := Revoked{SignInsByOrganization: map[string]int{}, WorkOSSessionIDs: nil, Sessions: nil, DeliveryIDs: nil}
	if len(organizationIDs) == 0 || userID == "" {
		return out, nil
	}
	q := repo.New(db)
	if _, err := q.LockUserSSOSignIns(ctx, repo.LockUserSSOSignInsParams{OrganizationIds: organizationIDs, UserID: userID}); err != nil {
		return out, fmt.Errorf("lock user sso sign-ins: %w", err)
	}
	if err := q.RaiseRevocationCutoffs(ctx, repo.RaiseRevocationCutoffsParams{UserID: userID, OrganizationIds: organizationIDs}); err != nil {
		return out, fmt.Errorf("raise sso sign-in revocation cutoffs: %w", err)
	}
	signIns, err := q.RevokeUserSSOSignIns(ctx, repo.RevokeUserSSOSignInsParams{
		RevokeReason:    conv.ToPGText(string(reason)),
		OrganizationIds: organizationIDs,
		UserID:          userID,
	})
	if err != nil {
		return out, fmt.Errorf("revoke user sso sign-ins: %w", err)
	}
	workosByOrg := map[string][]string{}
	seenSession := map[string]struct{}{}
	for _, s := range signIns {
		out.SignInsByOrganization[s.OrganizationID]++
		workosByOrg[s.OrganizationID] = append(workosByOrg[s.OrganizationID], s.WorkosSessionID)
		if _, ok := seenSession[s.WorkosSessionID]; !ok {
			seenSession[s.WorkosSessionID] = struct{}{}
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
	jtisByOrg := map[string][]string{}
	for _, s := range sessions {
		out.Sessions = append(out.Sessions, RevokedSession{
			ID:             s.ID,
			OrganizationID: s.OrganizationID.String,
			ProjectID:      s.ProjectID.UUID,
			Subject:        s.SubjectUrn,
			JTI:            s.Jti,
		})
		if s.Jti != "" {
			jtisByOrg[s.OrganizationID.String] = append(jtisByOrg[s.OrganizationID.String], s.Jti)
		}
	}
	for organizationID, jtis := range jtisByOrg {
		ids, err := enqueue(ctx, q, organizationID, DeliveryJTI, jtis)
		if err != nil {
			return out, err
		}
		out.DeliveryIDs = append(out.DeliveryIDs, ids...)
	}
	for organizationID, workosSessions := range workosByOrg {
		ids, err := enqueue(ctx, q, organizationID, DeliveryWorkOSSession, workosSessions)
		if err != nil {
			return out, err
		}
		out.DeliveryIDs = append(out.DeliveryIDs, ids...)
	}
	return out, nil
}

// EnqueueWorkOSSessionRevocations durably enqueues ending WorkOS sessions not
// tied to a recorded sign-in, such as ones listed from WorkOS.
func EnqueueWorkOSSessionRevocations(ctx context.Context, db repo.DBTX, organizationID string, workosSessionIDs []string) ([]uuid.UUID, error) {
	return enqueue(ctx, repo.New(db), organizationID, DeliveryWorkOSSession, workosSessionIDs)
}

func enqueue(ctx context.Context, q *repo.Queries, organizationID string, kind DeliveryKind, targets []string) ([]uuid.UUID, error) {
	targets = slices.Compact(slices.Sorted(slices.Values(targets)))
	targets = slices.DeleteFunc(targets, func(t string) bool { return t == "" })
	if organizationID == "" || len(targets) == 0 {
		return nil, nil
	}
	ids, err := q.EnqueueRevocationDeliveries(ctx, repo.EnqueueRevocationDeliveriesParams{
		OrganizationID: organizationID,
		Kind:           string(kind),
		Targets:        targets,
	})
	if err != nil {
		return nil, fmt.Errorf("enqueue %s revocation deliveries: %w", kind, err)
	}
	return ids, nil
}
