package admin

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// PostHog keys the organization group by slug, and analytics filter on this
// group property to leave internal organizations out.
const (
	posthogOrganizationGroup = "organization"
	posthogInternalProperty  = "is_internal"
)

// OrganizationGroupIdentifier sets properties on the PostHog organization group.
type OrganizationGroupIdentifier interface {
	GroupIdentify(ctx context.Context, groupType string, groupKey string, groupProperties map[string]any) error
}

// SetOrganizationGroupIdentifier wires the PostHog client. Nil leaves the
// internal flag unmirrored.
func (s *Service) SetOrganizationGroupIdentifier(identifier OrganizationGroupIdentifier) {
	s.organizationGroups = identifier
}

// mirrorOrganizationInternal pushes the label to PostHog after every write,
// changed or not, so saving again repairs a value PostHog missed. The database
// is the source of truth: a failed push is logged and never fails the write.
func (s *Service) mirrorOrganizationInternal(ctx context.Context, id string, slug string, internal bool) {
	if s.organizationGroups == nil {
		return
	}
	err := s.organizationGroups.GroupIdentify(ctx, posthogOrganizationGroup, slug, map[string]any{posthogInternalProperty: internal})
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to mirror organization internal flag to posthog",
			attr.SlogError(err),
			attr.SlogOrganizationID(id),
		)
	}
}

// setOrganizationInternal stores the staff-set internal label and returns the
// organization's slug. The label is internal staff metadata, so it is recorded
// in the server log rather than the organization's own audit log, which the
// organization's admins can read.
func (s *Service) setOrganizationInternal(ctx context.Context, id string, internal bool) (string, error) {
	slug, err := repo.New(s.db).AdminSetOrganizationInternal(ctx, repo.AdminSetOrganizationInternalParams{
		ID:       id,
		Internal: internal,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "set organization internal flag").LogError(ctx, s.logger)
	}

	actor, _, _ := adminActor(ctx)
	message := "admin marked organization internal"
	if !internal {
		message = "admin unmarked organization internal"
	}
	s.logger.InfoContext(ctx, message,
		attr.SlogOrganizationID(id),
		attr.SlogUserID(actor.ID),
	)

	return slug, nil
}
