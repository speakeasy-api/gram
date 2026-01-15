package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	customdomainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
)

type ListActiveCustomDomainsArgs struct {
	OrganizationID string
}

type ListActiveCustomDomains struct {
	domains *customdomainsRepo.Queries
	logger  *slog.Logger
}

func NewListActiveCustomDomains(logger *slog.Logger, db *pgxpool.Pool) *ListActiveCustomDomains {
	return &ListActiveCustomDomains{
		domains: customdomainsRepo.New(db),
		logger:  logger,
	}
}

func (l *ListActiveCustomDomains) Do(ctx context.Context, args ListActiveCustomDomainsArgs) ([]string, error) {
	var domains []customdomainsRepo.CustomDomain
	var err error

	if args.OrganizationID != "" {
		domains, err = l.domains.ListActiveCustomDomainsByOrg(ctx, args.OrganizationID)
	} else {
		domains, err = l.domains.ListActiveCustomDomains(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("list active custom domains: %w", err)
	}

	result := make([]string, len(domains))
	for i, d := range domains {
		result[i] = d.Domain
	}

	l.logger.InfoContext(ctx, "listed active custom domains",
		attr.SlogValueInt(len(result)))

	return result, nil
}
