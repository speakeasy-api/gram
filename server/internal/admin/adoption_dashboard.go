package admin

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const maxAdoptionDashboardURLLength = 2048

// normalizeAdoptionDashboardURL returns nil for an empty value, which clears
// the link, and otherwise requires an absolute https URL with a host. The
// length limit counts characters, matching the API's MaxLength and the
// database's char_length check.
func normalizeAdoptionDashboardURL(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	value := strings.TrimSpace(*raw)
	if value == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(value) > maxAdoptionDashboardURLLength {
		return nil, oops.E(oops.CodeBadRequest, nil, "adoption dashboard URL must be at most 2048 characters")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "adoption dashboard URL must be an https URL")
	}
	return &value, nil
}

// SetAdoptionDashboardURL stores the staff-entered link to an organization's
// adoption dashboard. It is internal staff metadata, so it is recorded in the
// server log rather than the organization's own audit log, which the
// organization's admins can read.
func (s *Service) SetAdoptionDashboardURL(ctx context.Context, payload *gen.SetAdoptionDashboardURLPayload) (*gen.AdminOrganization, error) {
	link, err := normalizeAdoptionDashboardURL(payload.AdoptionDashboardURL)
	if err != nil {
		return nil, err
	}

	_, err = repo.New(s.db).AdminSetOrganizationAdoptionDashboardURL(ctx, repo.AdminSetOrganizationAdoptionDashboardURLParams{
		ID:                   payload.OrganizationID,
		AdoptionDashboardUrl: conv.PtrToPGTextEmpty(link),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set adoption dashboard URL").LogError(ctx, s.logger)
	}

	actor, _, _ := adminActor(ctx)
	message := "admin set organization adoption dashboard URL"
	if link == nil {
		message = "admin cleared organization adoption dashboard URL"
	}
	s.logger.InfoContext(ctx, message,
		attr.SlogOrganizationID(payload.OrganizationID),
		attr.SlogUserID(actor.ID),
	)

	return s.readOrganizationAfterWrite(ctx, payload.OrganizationID, "read organization after setting adoption dashboard URL")
}
