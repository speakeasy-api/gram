package signins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/signins/repo"
)

// Flows that exchange a WorkOS code.
const (
	FlowDashboard    = "dashboard"
	FlowMCPAuthorize = "mcp_authorize"
)

// recordTimeout bounds recording, including waiting behind a revocation that
// holds the user's lock.
const recordTimeout = 5 * time.Second

// ExchangeParams describes a completed WorkOS code exchange.
type ExchangeParams struct {
	Flow string
	// WorkOSOrganizationID is the WorkOS organization selected during sign-in.
	WorkOSOrganizationID string
	// ExpectedOrganizationID, when set, is the only organization the sign-in
	// may be recorded in, e.g. the MCP endpoint's.
	ExpectedOrganizationID string
	UserID                 string
	WorkOSSessionID        string
	AuthenticationMethod   string
	Evidence               Evidence
	Impersonated           bool
	// AuthenticatedAt is when the exchange started, so a revocation that
	// lands while it is in flight fences it out.
	AuthenticatedAt time.Time
}

// RecordExchange records the sign-in for a WorkOS code exchange. It records
// only when the WorkOS-selected organization resolves locally (and matches
// ExpectedOrganizationID when set); anything else leaves the grant unlinked.
// It is best effort: failures are logged and counted, never returned.
func RecordExchange(ctx context.Context, logger *slog.Logger, db Beginner, p ExchangeParams) Recorded {
	none := Recorded{ID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, OrganizationID: "", Revoked: false, Failed: false, DeliveryIDs: nil}
	logger = logger.With(
		attr.SlogAuthSignInFlow(p.Flow),
		attr.SlogAuthAuthenticationMethod(p.AuthenticationMethod),
		attr.SlogAuthWorkOSSessionFingerprint(sessionFingerprint(p.WorkOSSessionID)),
	)
	logger.InfoContext(ctx, "workos code exchange completed")

	var organizationID string
	outcome := func(o string) Recorded {
		recordOutcome(ctx, p.Flow, o)
		failed := none
		// A WorkOS session held by another user is a recording failure too:
		// the login must not continue unlinked.
		failed.Failed = o == OutcomeError || o == OutcomeOtherUser
		failed.OrganizationID = organizationID
		return failed
	}
	switch {
	case p.Impersonated:
		return outcome(OutcomeImpersonated)
	case p.WorkOSSessionID == "" || p.UserID == "":
		return outcome(OutcomeNoSession)
	case p.WorkOSOrganizationID == "":
		return outcome(OutcomeNoOrganization)
	}

	ctx, cancel := context.WithTimeout(ctx, recordTimeout)
	defer cancel()

	resolved, err := repo.New(db).GetOrganizationIDByWorkOSID(ctx, conv.ToPGText(p.WorkOSOrganizationID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return outcome(OutcomeUnknownOrganization)
	case err != nil:
		logger.ErrorContext(ctx, "resolve organization for sign-in", attr.SlogError(err))
		return outcome(OutcomeError)
	case p.ExpectedOrganizationID != "" && resolved != p.ExpectedOrganizationID:
		return outcome(OutcomeOrganizationMismatch)
	}
	organizationID = resolved

	recorded, err := Record(ctx, db, RecordParams{
		OrganizationID:       organizationID,
		UserID:               p.UserID,
		WorkOSSessionID:      p.WorkOSSessionID,
		AuthenticationMethod: p.AuthenticationMethod,
		Evidence:             p.Evidence,
		AuthenticatedAt:      authenticatedAt(p.AuthenticatedAt),
	})
	switch {
	case errors.Is(err, errSignInOwnedByAnotherUser):
		logger.ErrorContext(ctx, "record sign-in", attr.SlogUserID(p.UserID), attr.SlogError(err))
		return outcome(OutcomeOtherUser)
	case err != nil:
		logger.ErrorContext(ctx, "record sign-in", attr.SlogUserID(p.UserID), attr.SlogError(err))
		return outcome(OutcomeError)
	case recorded.Revoked:
		recordOutcome(ctx, p.Flow, OutcomeRecordedRevoked)
	default:
		recordOutcome(ctx, p.Flow, OutcomeRecorded)
	}
	return recorded
}

func authenticatedAt(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
}

func sessionFingerprint(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:6])
}
