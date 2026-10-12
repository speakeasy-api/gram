package ssosignins

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
	"github.com/speakeasy-api/gram/server/internal/ssosignins/repo"
)

// Flows that exchange a WorkOS code.
const (
	FlowDashboard    = "dashboard"
	FlowMCPAuthorize = "mcp_authorize"
)

// recordTimeout bounds the best-effort recording so it never slows sign-in.
const recordTimeout = time.Second

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
	Impersonated           bool
}

// RecordExchange records the sign-in for a WorkOS code exchange. It records
// only when the WorkOS-selected organization resolves locally (and matches
// ExpectedOrganizationID when set); anything else leaves the grant unlinked.
// It is best effort: failures are logged and counted, never returned.
func RecordExchange(ctx context.Context, logger *slog.Logger, db repo.DBTX, p ExchangeParams) Recorded {
	none := Recorded{ID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Revoked: false}
	logger = logger.With(
		attr.SlogAuthSignInFlow(p.Flow),
		attr.SlogAuthAuthenticationMethod(p.AuthenticationMethod),
		attr.SlogAuthWorkOSSessionFingerprint(sessionFingerprint(p.WorkOSSessionID)),
	)
	logger.InfoContext(ctx, "workos code exchange completed")

	outcome := func(o string) Recorded {
		recordOutcome(ctx, p.Flow, o)
		return none
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

	organizationID, err := repo.New(db).GetOrganizationIDByWorkOSID(ctx, conv.ToPGText(p.WorkOSOrganizationID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return outcome(OutcomeUnknownOrganization)
	case err != nil:
		logger.ErrorContext(ctx, "resolve organization for sso sign-in", attr.SlogError(err))
		return outcome(OutcomeError)
	case p.ExpectedOrganizationID != "" && organizationID != p.ExpectedOrganizationID:
		return outcome(OutcomeOrganizationMismatch)
	}

	recorded, err := Record(ctx, db, RecordParams{
		OrganizationID:       organizationID,
		UserID:               p.UserID,
		WorkOSSessionID:      p.WorkOSSessionID,
		AuthenticationMethod: p.AuthenticationMethod,
		AuthenticatedAt:      time.Now(),
	})
	switch {
	case errors.Is(err, errSignInOwnedByAnotherUser):
		logger.ErrorContext(ctx, "record sso sign-in", attr.SlogUserID(p.UserID), attr.SlogError(err))
		return outcome(OutcomeOtherUser)
	case err != nil:
		logger.ErrorContext(ctx, "record sso sign-in", attr.SlogUserID(p.UserID), attr.SlogError(err))
		return outcome(OutcomeError)
	case recorded.Revoked:
		recordOutcome(ctx, p.Flow, OutcomeRecordedRevoked)
	default:
		recordOutcome(ctx, p.Flow, OutcomeRecorded)
	}
	return recorded
}

func sessionFingerprint(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:6])
}
