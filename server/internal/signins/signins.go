// Package signins records the sign-ins that authorize MCP grants and judges
// whether a grant still traces to a current, revocable one of an accepted tier.
package signins

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
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/signins/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// MaxAge is the longest any sign-in can back a grant: the SSO tier's bound.
// TODO: prune revoked and expired sign_ins rows (retention sweep follow-up).
const MaxAge = 7 * 24 * time.Hour

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
	ReasonMethodNotEligible    Reason = "sign_in_method_not_eligible"
	ReasonUnmanagedAccount     Reason = "unmanaged_account"
	ReasonMethodNotAllowed     Reason = "sign_in_method_not_allowed"
	ReasonExpired              Reason = "expired"
	ReasonOrganizationDisabled Reason = "organization_disabled"
	ReasonNotMember            Reason = "not_member"
	ReasonDeprovisioned        Reason = "deprovisioned"
	ReasonEmailDomain          Reason = "email_domain_not_verified"
	ReasonEmailNotVerified     Reason = "email_not_verified"
	ReasonEmailChanged         Reason = "email_changed"
	ReasonAuthTimeUnknown      Reason = "authentication_time_unknown"
	ReasonTenantNotAllowed     Reason = "microsoft_tenant_not_allowed"
)

// Identity is the verified identity of an eligible grant.
type Identity struct {
	UserID   string
	Email    string
	SignInID uuid.UUID
	// Tier is the sign-in tier that backs the identity, e.g. TierSSO.
	Tier string
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
	Evidence             Evidence
	AuthenticatedAt      time.Time
}

// Evidence is what the identity provider vouched for at sign-in, beyond the
// method itself.
type Evidence struct {
	// HostedDomain is the domain a social provider says manages the account:
	// the Google Workspace hosted domain or a Microsoft work account's UPN domain.
	HostedDomain string
	// TenantID is a Microsoft account's Entra tenant.
	TenantID string
	// VerifiedEmail is the email the identity provider verified, empty when
	// it did not. Only this email is ever vouched for.
	VerifiedEmail string
	// IDPAuthenticatedAt is when the user actually authenticated at the
	// identity provider, zero when unknown. A reused session keeps its time.
	IDPAuthenticatedAt time.Time
}

// Recorded is the sign-in a code exchange resolved to. A revoked row is still
// returned, so a grant made from it stays reachable by later revocations and
// is never vouched for.
type Recorded struct {
	ID             uuid.NullUUID
	OrganizationID string
	Revoked        bool
	// Failed is set when recording was attempted and failed, so a caller
	// enforcing linkage can refuse to continue unlinked.
	Failed bool
	// DeliveryIDs end the WorkOS session of a sign-in recorded revoked.
	DeliveryIDs []uuid.UUID
}

var errSignInOwnedByAnotherUser = errors.New("sign-in session belongs to another user")

// Beginner is a pool or transaction that Record runs its own transaction on.
type Beginner interface {
	repo.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Record upserts the sign-in for a WorkOS session under the user's
// per-organization lock, so it serializes with revocation. It records nothing
// when the exchange carries no session, organization or user.
func Record(ctx context.Context, db Beginner, p RecordParams) (Recorded, error) {
	none := Recorded{ID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, OrganizationID: "", Revoked: false, Failed: false, DeliveryIDs: nil}
	if p.OrganizationID == "" || p.UserID == "" || p.WorkOSSessionID == "" {
		return none, nil
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return none, fmt.Errorf("begin: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)
	if err := lockUser(ctx, q, []string{p.OrganizationID}, p.UserID); err != nil {
		return none, err
	}
	var idpAuthenticatedAt pgtype.Timestamptz
	if !p.Evidence.IDPAuthenticatedAt.IsZero() {
		idpAuthenticatedAt = pgtype.Timestamptz{Time: p.Evidence.IDPAuthenticatedAt, InfinityModifier: pgtype.Finite, Valid: true}
	}
	row, err := q.UpsertSignIn(ctx, repo.UpsertSignInParams{
		OrganizationID:       p.OrganizationID,
		UserID:               p.UserID,
		WorkosSessionID:      p.WorkOSSessionID,
		AuthenticationMethod: p.AuthenticationMethod,
		IdpHostedDomain:      conv.ToPGTextEmpty(p.Evidence.HostedDomain),
		IdpTenantID:          conv.ToPGTextEmpty(p.Evidence.TenantID),
		VerifiedEmail:        conv.ToPGTextEmpty(conv.NormalizeEmail(p.Evidence.VerifiedEmail)),
		AuthenticatedAt:      pgtype.Timestamptz{Time: p.AuthenticatedAt, InfinityModifier: pgtype.Finite, Valid: true},
		IdpAuthenticatedAt:   idpAuthenticatedAt,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return none, errSignInOwnedByAnotherUser
	case err != nil:
		return none, fmt.Errorf("upsert sign-in: %w", err)
	}
	// A sign-in recorded already revoked (fenced or signed out) must not
	// leave its WorkOS session usable, unless another organization still
	// relies on it.
	var deliveryIDs []uuid.UUID
	if row.RevokedAt.Valid {
		if deliveryIDs, err = enqueueEndingWorkOSSessions(ctx, q, p.OrganizationID, userIDSessions{userID: p.UserID, organizationIDs: []string{p.OrganizationID}}, []string{p.WorkOSSessionID}); err != nil {
			return none, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return none, fmt.Errorf("commit: %w", err)
	}
	return Recorded{ID: uuid.NullUUID{UUID: row.ID, Valid: true}, OrganizationID: p.OrganizationID, Revoked: row.RevokedAt.Valid, Failed: false, DeliveryIDs: deliveryIDs}, nil
}

// LiveSignInForSession returns the unrevoked sign-in recorded for a WorkOS
// session in an organization, or an invalid id when there is none.
func LiveSignInForSession(ctx context.Context, db repo.DBTX, organizationID, userID, workosSessionID string) (uuid.NullUUID, error) {
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if organizationID == "" || userID == "" || workosSessionID == "" {
		return none, nil
	}
	id, err := repo.New(db).GetLiveSignInBySession(ctx, repo.GetLiveSignInBySessionParams{
		OrganizationID:  organizationID,
		UserID:          userID,
		WorkosSessionID: workosSessionID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return none, nil
	case err != nil:
		return none, fmt.Errorf("get sign-in for session: %w", err)
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
	id, err := repo.New(db).GetSignInBySession(ctx, repo.GetSignInBySessionParams{
		OrganizationID:  organizationID,
		UserID:          userID,
		WorkosSessionID: workosSessionID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return none, nil
	case err != nil:
		return none, fmt.Errorf("get sign-in for session: %w", err)
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

// CheckSignInIdentity judges whether an MCP grant traces to a current sign-in
// whose user Speakeasy may vouch for in organizationID. The sign-in's tier sets
// how old it may be; a positive maxAge can only shorten that. A grant without a
// recorded sign-in is ineligible, never an error.
func CheckSignInIdentity(ctx context.Context, db repo.DBTX, organizationID string, userSessionID uuid.UUID, now time.Time, maxAge time.Duration) (Result, error) {
	row, err := repo.New(db).GetSignInEligibility(ctx, repo.GetSignInEligibilityParams{
		OrganizationID:    organizationID,
		UserSessionID:     userSessionID,
		ConsentSignInID:   uuid.Nil,
		ConsentSubjectUrn: "",
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Result{Reason: ReasonSessionNotFound, Identity: nil}, nil
	case err != nil:
		return Result{Reason: ReasonSessionNotFound, Identity: nil}, fmt.Errorf("get sign-in eligibility: %w", err)
	}
	return judge(row, now, maxAge), nil
}

// CheckConsentIdentity checks the exact sign-in captured by an authenticated
// authorization flow, before that flow has created an MCP grant. The sign-in
// tier's own age limit applies; the caller sets no additional cap. Identity
// chaining relies on it to mint and store a vendor token for that sign-in on
// the consent page; the token is released only to a grant whose own sign-in
// then passes CheckSignInIdentity.
func CheckConsentIdentity(ctx context.Context, db repo.DBTX, organizationID, userID string, signInID uuid.UUID, now time.Time) (Result, error) {
	if signInID == uuid.Nil || userID == "" {
		return Result{Reason: ReasonNotLinked, Identity: nil}, nil
	}
	row, err := repo.New(db).GetSignInEligibility(ctx, repo.GetSignInEligibilityParams{
		OrganizationID:    organizationID,
		UserSessionID:     uuid.Nil,
		ConsentSignInID:   signInID,
		ConsentSubjectUrn: urn.NewUserSubject(userID).String(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{Reason: ReasonNotLinked, Identity: nil}, nil
	}
	if err != nil {
		return Result{Reason: ReasonNotLinked, Identity: nil}, fmt.Errorf("get consent sign-in eligibility: %w", err)
	}
	return judge(row, now, 0), nil
}

func judge(row repo.GetSignInEligibilityRow, now time.Time, maxAge time.Duration) Result {
	reject := func(reason Reason) Result { return Result{Reason: reason, Identity: nil} }
	if !row.SessionLive {
		return reject(ReasonSessionExpired)
	}
	if !row.LinkedSignInID.Valid || !row.SignInID.Valid {
		return reject(ReasonNotLinked)
	}
	userID := row.SignInUserID.String
	if row.SubjectUrn != urn.NewUserSubject(userID).String() {
		return reject(ReasonNotUserSubject)
	}
	if row.RevokedAt.Valid || row.Fenced {
		return reject(ReasonSignInRevoked)
	}
	tier, reason := classify(row)
	if reason != ReasonEligible {
		return reject(reason)
	}
	if !tierAllowed(tier, row) {
		return reject(ReasonMethodNotAllowed)
	}
	if limit := TierMaxAge(tier); maxAge <= 0 || maxAge > limit {
		maxAge = limit
	}
	switch {
	case !row.IdpAuthenticatedAt.Valid:
		return reject(ReasonAuthTimeUnknown)
	case now.Sub(row.IdpAuthenticatedAt.Time) > maxAge:
		return reject(ReasonExpired)
	case row.OrganizationDisabledAt.Valid:
		return reject(ReasonOrganizationDisabled)
	case !row.IsMember || !row.UserEmail.Valid:
		return reject(ReasonNotMember)
	case row.IsDeprovisioned:
		return reject(ReasonDeprovisioned)
	case !row.VerifiedEmail.Valid || row.VerifiedEmail.String == "":
		return reject(ReasonEmailNotVerified)
	}
	// Vouch only for the email verified at this sign-in; a later profile
	// change waits for a fresh sign-in.
	email := conv.NormalizeEmail(row.VerifiedEmail.String)
	if conv.NormalizeEmail(row.UserEmail.String) != email {
		return reject(ReasonEmailChanged)
	}
	if !emailOnDomain(email, row.VerifiedDomains) {
		return reject(ReasonEmailDomain)
	}
	return Result{Reason: ReasonEligible, Identity: &Identity{UserID: userID, Email: email, SignInID: row.SignInID.UUID, Tier: tier}}
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
	// SignIns counts the sign-ins this sign-out actually revoked; a marker
	// for a session with nothing recorded counts zero.
	SignIns int
	// Organizations hold the durable deliveries it enqueued.
	Organizations []string
	// DeliveryIDs are the durable deliveries it enqueued.
	DeliveryIDs []uuid.UUID
}

// RevokeSession revokes the sign-ins recorded for one signed-out WorkOS
// session and enqueues ending that WorkOS session. Grants made from them keep
// working but stop qualifying for vouching. Locks are taken in id order, like
// every other revocation. When nothing was recorded yet, the first
// organization keeps a revoked marker that a later recording inherits. The
// user's sign-in organizations are re-listed under the user lock, so a
// recording that committed just before is revoked too.
func RevokeSession(ctx context.Context, db repo.DBTX, organizationIDs []string, userID, workosSessionID string) (SignedOut, error) {
	var out SignedOut
	if len(organizationIDs) == 0 || userID == "" || workosSessionID == "" {
		return out, nil
	}
	q := repo.New(db)
	if err := q.LockAllSignInsForUser(ctx, userID); err != nil {
		return out, fmt.Errorf("lock user sign-ins: %w", err)
	}
	recorded, err := q.ListSignInOrganizationsForUser(ctx, userID)
	if err != nil {
		return out, fmt.Errorf("list sign-in organizations: %w", err)
	}
	for _, id := range recorded {
		if !slices.Contains(organizationIDs, id) {
			organizationIDs = append(slices.Clone(organizationIDs), id)
		}
	}
	if err := lockUser(ctx, q, organizationIDs, userID); err != nil {
		return out, err
	}
	if _, err := q.LockSignInsBySession(ctx, repo.LockSignInsBySessionParams{OrganizationIds: organizationIDs, UserID: userID, WorkosSessionID: workosSessionID}); err != nil {
		return out, fmt.Errorf("lock sign-ins for session: %w", err)
	}
	rows, err := q.RevokeSignInsBySession(ctx, repo.RevokeSignInsBySessionParams{
		RevokeReason:    conv.ToPGText(string(RevokeReasonSignOut)),
		OrganizationIds: organizationIDs,
		UserID:          userID,
		WorkosSessionID: workosSessionID,
	})
	if err != nil {
		return out, fmt.Errorf("revoke sign-ins for session: %w", err)
	}
	if len(rows) == 0 {
		if err := q.MarkWorkOSSessionSignedOut(ctx, repo.MarkWorkOSSessionSignedOutParams{OrganizationID: organizationIDs[0], UserID: userID, WorkosSessionID: workosSessionID}); err != nil {
			return out, fmt.Errorf("mark workos session signed out: %w", err)
		}
		ids, err := enqueue(ctx, q, organizationIDs[0], DeliveryWorkOSSession, []string{workosSessionID})
		if err != nil {
			return out, err
		}
		out.Organizations = []string{organizationIDs[0]}
		out.DeliveryIDs = ids
		return out, nil
	}
	seen := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		if r.WasLive {
			out.SignIns++
		}
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
	orgs, err := repo.New(db).ListSignInOrganizationsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list sign-in organizations: %w", err)
	}
	return orgs, nil
}

// Revoked is what RevokeUser revoked.
type Revoked struct {
	// SignInsByOrganization counts newly revoked or upgraded sign-ins per
	// organization.
	SignInsByOrganization map[string]int
	// WorkOSSessionIDs are the WorkOS sessions of those sign-ins, including
	// ones left up because another organization still uses them.
	WorkOSSessionIDs []string
	// Sessions are the soft-deleted grants, linked to a revoked sign-in or
	// unlinked.
	Sessions []RevokedSession
	// CutoffAt is the revocation boundary: sign-ins and dashboard sessions
	// from up to then are revoked.
	CutoffAt time.Time
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
	out := Revoked{SignInsByOrganization: map[string]int{}, WorkOSSessionIDs: nil, Sessions: nil, CutoffAt: time.Time{}, DeliveryIDs: nil}
	if len(organizationIDs) == 0 || userID == "" {
		return out, nil
	}
	q := repo.New(db)
	if err := q.LockAllSignInsForUser(ctx, userID); err != nil {
		return out, fmt.Errorf("lock user sign-ins: %w", err)
	}
	// Lock every organization the user has signed in to, not only the ones
	// being revoked, so recordings elsewhere wait for the check below that
	// keeps their WorkOS sessions up. Listed under the user lock, so a
	// recording either committed already or waits for this revocation.
	recorded, err := q.ListSignInOrganizationsForUser(ctx, userID)
	if err != nil {
		return out, fmt.Errorf("list sign-in organizations: %w", err)
	}
	if err := lockUser(ctx, q, slices.Concat(organizationIDs, recorded), userID); err != nil {
		return out, err
	}
	if _, err := q.LockUserSignIns(ctx, repo.LockUserSignInsParams{OrganizationIds: organizationIDs, UserID: userID}); err != nil {
		return out, fmt.Errorf("lock user sign-ins: %w", err)
	}
	cutoffs, err := q.RaiseRevocationCutoffs(ctx, repo.RaiseRevocationCutoffsParams{UserID: userID, OrganizationIds: organizationIDs})
	if err != nil {
		return out, fmt.Errorf("raise sign-in revocation cutoffs: %w", err)
	}
	for _, c := range cutoffs {
		if c.Valid && c.Time.After(out.CutoffAt) {
			out.CutoffAt = c.Time
		}
	}
	signIns, err := q.RevokeUserSignIns(ctx, repo.RevokeUserSignInsParams{
		RevokeReason:    conv.ToPGText(string(reason)),
		OrganizationIds: organizationIDs,
		UserID:          userID,
	})
	if err != nil {
		return out, fmt.Errorf("revoke user sign-ins: %w", err)
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
		return out, fmt.Errorf("revoke grants for user sign-ins: %w", err)
	}
	unlinked, err := q.RevokeUnlinkedUserSessions(ctx, repo.RevokeUnlinkedUserSessionsParams{
		OrganizationIds: organizationIDs,
		SubjectUrn:      urn.NewUserSubject(userID),
	})
	if err != nil {
		return out, fmt.Errorf("revoke unlinked grants: %w", err)
	}
	for _, s := range unlinked {
		sessions = append(sessions, repo.RevokeUserSessionsForUserSignInsRow(s))
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
		ids, err := enqueueEndingWorkOSSessions(ctx, q, organizationID, userIDSessions{userID: userID, organizationIDs: organizationIDs}, workosSessions)
		if err != nil {
			return out, err
		}
		out.DeliveryIDs = append(out.DeliveryIDs, ids...)
	}
	return out, nil
}

// EnqueueUserEpoch durably enqueues ending the user's dashboard sessions
// issued at or before at; a pending one only moves forward.
func EnqueueUserEpoch(ctx context.Context, db repo.DBTX, organizationID, userID string, at time.Time) (uuid.UUID, error) {
	id, err := repo.New(db).EnqueueUserEpochDelivery(ctx, repo.EnqueueUserEpochDeliveryParams{
		OrganizationID: organizationID,
		UserID:         userID,
		EffectiveAt:    pgtype.Timestamptz{Time: at, InfinityModifier: pgtype.Finite, Valid: true},
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("enqueue user epoch delivery: %w", err)
	}
	return id, nil
}

// userIDSessions names whose WorkOS sessions are being ended, and the
// organizations whose own sign-ins on them don't count as other use.
type userIDSessions struct {
	userID          string
	organizationIDs []string
}

// enqueueEndingWorkOSSessions enqueues ending each WorkOS session that backs
// no live sign-in outside the given organizations; the others stay up.
func enqueueEndingWorkOSSessions(ctx context.Context, q *repo.Queries, organizationID string, owner userIDSessions, workosSessionIDs []string) ([]uuid.UUID, error) {
	ended := make([]string, 0, len(workosSessionIDs))
	for _, workosSessionID := range workosSessionIDs {
		used, err := workOSSessionUsedElsewhere(ctx, q, owner.userID, workosSessionID, owner.organizationIDs)
		if err != nil {
			return nil, err
		}
		if !used {
			ended = append(ended, workosSessionID)
		}
	}
	return enqueue(ctx, q, organizationID, DeliveryWorkOSSession, ended)
}

func workOSSessionUsedElsewhere(ctx context.Context, q *repo.Queries, userID, workosSessionID string, organizationIDs []string) (bool, error) {
	others, err := q.OtherOrganizationsUsingWorkOSSession(ctx, repo.OtherOrganizationsUsingWorkOSSessionParams{
		UserID:          userID,
		WorkosSessionID: workosSessionID,
		OrganizationIds: organizationIDs,
	})
	if err != nil {
		return false, fmt.Errorf("check workos session use elsewhere: %w", err)
	}
	return len(others) > 0, nil
}

// LockUser takes the user-wide sign-in lock, so no sign-in of the user is
// recorded until the transaction ends.
func LockUser(ctx context.Context, db repo.DBTX, userID string) error {
	if err := repo.New(db).LockAllSignInsForUser(ctx, userID); err != nil {
		return fmt.Errorf("lock user sign-ins: %w", err)
	}
	return nil
}

// lockUser takes the user-wide sign-in lock, then the per-organization ones in
// sorted order, before any row lock. Every recording and revocation uses this
// order.
func lockUser(ctx context.Context, q *repo.Queries, organizationIDs []string, userID string) error {
	if err := q.LockAllSignInsForUser(ctx, userID); err != nil {
		return fmt.Errorf("lock user sign-ins: %w", err)
	}
	for _, organizationID := range slices.Compact(slices.Sorted(slices.Values(organizationIDs))) {
		if err := q.LockSignInsForUser(ctx, repo.LockSignInsForUserParams{OrganizationID: organizationID, UserID: userID}); err != nil {
			return fmt.Errorf("lock sign-ins: %w", err)
		}
	}
	return nil
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
