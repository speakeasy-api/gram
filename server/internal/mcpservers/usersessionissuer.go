package mcpservers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const (
	mintedIssuerSessionDuration      = 14 * 24 * time.Hour
	userSessionResourceSlugMaxLen    = 40
	userSessionResourceSlugSuffixLen = 8
)

// Keep this aligned with buildUserSessionResourceSlug in the dashboard. It
// preserves the issuer naming used before issuer creation moved server-side.
func buildUserSessionResourceSlug(baseSlug string) (string, error) {
	suffix, err := conv.GenerateRandomSlug(userSessionResourceSlugSuffixLen)
	if err != nil {
		return "", fmt.Errorf("generate user session resource slug suffix: %w", err)
	}

	normalizedBase := conv.ToSlug(baseSlug)
	if normalizedBase == "" {
		normalizedBase = "mcp"
	}

	maxBaseLen := userSessionResourceSlugMaxLen - len(suffix) - 1
	if len(normalizedBase) > maxBaseLen {
		normalizedBase = normalizedBase[:maxBaseLen]
	}
	normalizedBase = strings.TrimRight(normalizedBase, "-")
	if normalizedBase == "" {
		normalizedBase = "mcp"
	}

	return normalizedBase + "-" + suffix, nil
}

// mintServerUserSessionIssuer creates the user_session_issuer a remote- or
// tunneled-backed server carries for its lifetime, inside the caller's create
// transaction so a failed create can never leak an orphan issuer. The issuer
// is slugged after the server, so it reads naturally in the issuer list and
// stays unique per server.
func mintServerUserSessionIssuer(
	ctx context.Context,
	dbtx pgx.Tx,
	projectID uuid.UUID,
	serverSlug string,
) (uuid.NullUUID, error) {
	issuerSlug, err := buildUserSessionResourceSlug(serverSlug)
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, err
	}

	issuer, err := usersessionsrepo.New(dbtx).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		Slug:               issuerSlug,
		AuthnChallengeMode: "interactive",
		SessionDuration: pgtype.Interval{
			Microseconds: mintedIssuerSessionDuration.Microseconds(),
			Days:         0,
			Months:       0,
			Valid:        true,
		},
	})
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, fmt.Errorf("create user session issuer: %w", err)
	}
	return uuid.NullUUID{UUID: issuer.ID, Valid: true}, nil
}
