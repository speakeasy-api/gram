package toolsets

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// resolveCustomDomain keeps the toolset's domain unless the request names the organization's active, verified one.
func (s *Service) resolveCustomDomain(ctx context.Context, dbtx pgx.Tx, logger *slog.Logger, authCtx *contextvalues.AuthContext, existing repo.Toolset, requestedDomainID *string) (uuid.NullUUID, error) {
	if requestedDomainID == nil {
		return existing.CustomDomainID, nil
	}
	domain, err := s.domainsRepo.WithTx(dbtx).GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return existing.CustomDomainID, nil
	case err != nil:
		return uuid.NullUUID{}, oops.E(oops.CodeUnexpected, err, "load organization custom domain").LogError(ctx, logger)
	case domain.Activated && domain.Verified && *requestedDomainID == domain.ID.String():
		return uuid.NullUUID{UUID: domain.ID, Valid: true}, nil
	default:
		return existing.CustomDomainID, nil
	}
}

// resolveMCPAddress validates a requested mcp_slug on a locked toolset's resolved domain under the toolset API's rules.
func (s *Service) resolveMCPAddress(ctx context.Context, dbtx pgx.Tx, logger *slog.Logger, authCtx *contextvalues.AuthContext, existing repo.Toolset, requestedSlug *string, customDomainID uuid.NullUUID) (pgtype.Text, error) {
	mcpSlug := existing.McpSlug
	toolsetDomainID := conv.FromNullableUUID(customDomainID)

	slugChanged := requestedSlug != nil && *requestedSlug != ""
	domainChanged := customDomainID.Valid &&
		(!existing.CustomDomainID.Valid || existing.CustomDomainID.UUID != customDomainID.UUID)
	slug := existing.McpSlug.String
	if slugChanged {
		slug = conv.ToLower(*requestedSlug)
	}

	// Probe whenever the effective (slug, domain) address changes: a domain
	// change alone re-scopes the existing slug and can collide there too.
	if slug != "" && (slugChanged || domainChanged) {
		domainID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		// Slugs on the platform domain (no custom domain, or free accounts) must be prefixed with the org slug
		if toolsetDomainID == nil || authCtx.AccountType == "free" {
			if !strings.HasPrefix(slug, authCtx.OrganizationSlug+"-") {
				return pgtype.Text{}, oops.E(oops.CodeBadRequest, nil, "mcp slug must be prefixed with the org slug for free accounts")
			}
		} else {
			domainID = uuid.NullUUID{UUID: uuid.MustParse(*toolsetDomainID), Valid: true}
		}

		// Domain rows lock before the slug scope, the order endpoint writers use.
		if _, err := hostedmcp.LockDomains(ctx, dbtx, authCtx.ActiveOrganizationID, existing.CustomDomainID, domainID); err != nil {
			return pgtype.Text{}, oops.E(oops.CodeUnexpected, err, "lock custom domains").LogError(ctx, logger)
		}
		if err := mcpendpoints.LockSlugScope(ctx, dbtx, domainID, slug); err != nil {
			return pgtype.Text{}, oops.E(oops.CodeUnexpected, err, "lock mcp slug scope").LogError(ctx, logger)
		}
		// The scope is the toolset's own domain or the org's verified one, so
		// the ownership guard is skipped: a soft-deleted domain must not block
		// renames within its scope.
		available, err := mcpendpoints.CheckSlugAvailable(ctx, dbtx, mcpendpoints.SlugAvailabilityCheck{
			Slug:                     slug,
			CustomDomainID:           domainID,
			OrganizationID:           authCtx.ActiveOrganizationID,
			ExcludeToolsetID:         uuid.NullUUID{UUID: existing.ID, Valid: true},
			ExcludeMcpServerID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			SkipDomainOwnershipCheck: true,
		})
		if err != nil {
			return pgtype.Text{}, oops.E(oops.CodeUnexpected, err, "check mcp slug availability").LogError(ctx, logger)
		}
		if !available {
			return pgtype.Text{}, oops.E(oops.CodeConflict, nil, "this slug is already taken")
		}
		if slugChanged {
			mcpSlug = conv.ToPGText(slug)
		}
	}

	return mcpSlug, nil
}
