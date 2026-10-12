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

// ExchangeParams describes a completed WorkOS code exchange.
type ExchangeParams struct {
	Flow string
	// WorkOSOrganizationID is the WorkOS organization selected during the
	// sign-in; it names the organization the sign-in belongs to when known.
	WorkOSOrganizationID string
	// FallbackOrganizationID is used when the WorkOS organization is unknown,
	// e.g. the MCP endpoint's organization.
	FallbackOrganizationID string
	UserID                 string
	WorkOSSessionID        string
	AuthenticationMethod   string
}

// RecordExchange records the sign-in for a WorkOS code exchange and returns
// its id. It is best effort: failures are logged and yield an invalid id, so
// sign-in never fails because the record could not be written.
func RecordExchange(ctx context.Context, logger *slog.Logger, db repo.DBTX, p ExchangeParams) uuid.NullUUID {
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	logger = logger.With(
		attr.SlogAuthSignInFlow(p.Flow),
		attr.SlogAuthAuthenticationMethod(p.AuthenticationMethod),
		attr.SlogAuthWorkOSSessionFingerprint(sessionFingerprint(p.WorkOSSessionID)),
	)
	// Debug only: lets a real environment show whether the dashboard and MCP
	// authorize share one WorkOS session.
	logger.DebugContext(ctx, "workos code exchange completed")

	organizationID := p.FallbackOrganizationID
	if p.WorkOSOrganizationID != "" {
		id, err := repo.New(db).GetOrganizationIDByWorkOSID(ctx, conv.ToPGText(p.WorkOSOrganizationID))
		switch {
		case err == nil:
			organizationID = id
		case errors.Is(err, pgx.ErrNoRows):
		default:
			logger.ErrorContext(ctx, "resolve organization for sso sign-in", attr.SlogError(err))
			return none
		}
	}
	id, err := Record(ctx, db, RecordParams{
		OrganizationID:       organizationID,
		UserID:               p.UserID,
		WorkOSSessionID:      p.WorkOSSessionID,
		AuthenticationMethod: p.AuthenticationMethod,
		AuthenticatedAt:      time.Now(),
	})
	if err != nil {
		logger.ErrorContext(ctx, "record sso sign-in", attr.SlogError(err))
		return none
	}
	return id
}

func sessionFingerprint(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:6])
}
