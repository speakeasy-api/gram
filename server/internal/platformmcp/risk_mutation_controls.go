package platformmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/feature"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

const (
	operationCreateRiskPolicy    = "create_risk_policy"
	operationUpdateRiskPolicy    = "update_risk_policy"
	operationCreateRiskExclusion = "create_risk_exclusion"
	operationUpdateRiskExclusion = "update_risk_exclusion"
)

var (
	ErrRiskMutationUnavailable = errors.New("platform mcp risk mutations unavailable")
	ErrRiskMutationInvalid     = errors.New("invalid platform mcp risk mutation")
	ErrRiskMutationNotFound    = errors.New("platform mcp risk mutation target not found")
	ErrRiskMutationConflict    = errors.New("platform mcp risk mutation conflict")
)

// RiskMutationError is safe to map into a tool refusal. Message deliberately
// contains no policy, prompt, URL, principal, CEL, or exclusion material.
type RiskMutationError struct {
	Code    string
	Message string
	Cause   error
}

func (e *RiskMutationError) Error() string { return e.Message }
func (e *RiskMutationError) Unwrap() error { return e.Cause }

// OrganizationSlugResolver is the narrow organization lookup needed to target
// the exact-project risk mutation kill switch. It remains separate from the
// product-feature-only Platform MCP organization gate.
type OrganizationSlugResolver interface {
	OrganizationSlug(ctx context.Context, organizationID string) (string, error)
}

type PostgresOrganizationSlugResolver struct {
	db *pgxpool.Pool
}

func NewPostgresOrganizationSlugResolver(db *pgxpool.Pool) *PostgresOrganizationSlugResolver {
	return &PostgresOrganizationSlugResolver{db: db}
}

func (r *PostgresOrganizationSlugResolver) OrganizationSlug(ctx context.Context, organizationID string) (string, error) {
	if r == nil || r.db == nil || organizationID == "" {
		return "", ErrRiskMutationUnavailable
	}
	organization, err := organizationsrepo.New(r.db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return "", fmt.Errorf("get organization for Platform MCP risk mutation: %w", err)
	}
	return organization.Slug, nil
}

// RiskMutationControls owns the checks shared by every risk write. Admission is
// intentionally separate from receipt execution so callers can prove the flag
// and budget fail before opening a write transaction.
type RiskMutationControls struct {
	flags         feature.Provider
	organizations OrganizationSlugResolver
	projects      riskProjectResolver
	budget        OperationBudget
	receipts      *RiskMutationReceiptStore
	versions      *riskVersionCodec
}

func NewRiskMutationControls(db *pgxpool.Pool, flags feature.Provider, organizations OrganizationSlugResolver, budget OperationBudget, keyMaterial string) (*RiskMutationControls, error) {
	if db == nil || organizations == nil || keyMaterial == "" {
		return nil, ErrRiskMutationUnavailable
	}
	versions, err := newRiskVersionCodec(keyMaterial)
	if err != nil {
		return nil, err
	}
	return &RiskMutationControls{
		flags:         flags,
		organizations: organizations,
		projects:      postgresRiskProjectResolver{queries: platformrepo.New(db)},
		budget:        budget,
		receipts:      NewRiskMutationReceiptStore(db),
		versions:      versions,
	}, nil
}

// Admit resolves an explicit project and checks its exact rollout cohort at
// invocation time. Missing providers, errors, and indeterminate evaluations all
// fail closed. The mutation budget is consumed only after the kill switch is on.
func (c *RiskMutationControls) Admit(ctx context.Context, principal Principal, projectSlug string) (ResolvedProject, error) {
	if c == nil || c.flags == nil || c.organizations == nil || c.projects == nil || !c.budget.valid() || c.receipts == nil || c.versions == nil {
		return ResolvedProject{}, riskMutationUnavailable()
	}
	if principal.OrganizationID == "" || principal.UserID == "" || projectSlug == "" {
		return ResolvedProject{}, &RiskMutationError{Code: "invalid_request", Message: "An exact project and attributable user are required for risk mutations.", Cause: ErrRiskMutationInvalid}
	}
	project, err := c.projects.Resolve(ctx, principal.OrganizationID, "", projectSlug)
	if errors.Is(err, ErrRiskReadNotFound) {
		return ResolvedProject{}, &RiskMutationError{Code: "not_found", Message: "The requested risk mutation project is not available.", Cause: fmt.Errorf("%w: %w", ErrRiskMutationNotFound, err)}
	}
	if err != nil {
		return ResolvedProject{}, riskMutationUnavailableWithCause(err)
	}
	organizationSlug, err := c.organizations.OrganizationSlug(ctx, principal.OrganizationID)
	if err != nil {
		return ResolvedProject{}, riskMutationUnavailableWithCause(err)
	}
	if organizationSlug == "" {
		return ResolvedProject{}, riskMutationUnavailableWithCause(errors.New("organization slug is unavailable"))
	}
	evaluation, err := feature.EvaluateFlag(ctx, c.flags, feature.FlagPlatformMCPRiskMutations, principal.OrganizationID, feature.OrgProjectGroups(organizationSlug, project.Slug))
	if err != nil {
		return ResolvedProject{}, riskMutationUnavailableWithCause(err)
	}
	if evaluation != feature.EvaluationEnabled {
		return ResolvedProject{}, riskMutationUnavailable()
	}
	if err := c.budget.AllowConnectionOrOrganization(ctx, principal); err != nil {
		switch {
		case errors.Is(err, ErrOperationRateLimited):
			return ResolvedProject{}, &RiskMutationError{Code: "rate_limited", Message: "The risk mutation rate limit was reached.", Cause: err}
		default:
			return ResolvedProject{}, riskMutationUnavailableWithCause(err)
		}
	}
	return project, nil
}

func riskMutationUnavailable() error {
	return riskMutationUnavailableWithCause(nil)
}

func riskMutationUnavailableWithCause(cause error) error {
	if cause == nil {
		return &RiskMutationError{Code: unavailableCode, Message: "Risk mutations are not enabled for this project.", Cause: ErrRiskMutationUnavailable}
	}
	return &RiskMutationError{Code: "unavailable", Message: "Risk mutations are temporarily unavailable.", Cause: fmt.Errorf("%w: %w", ErrRiskMutationUnavailable, cause)}
}

func riskMutationConflict(message string) error {
	return &RiskMutationError{Code: "conflict", Message: message, Cause: ErrRiskMutationConflict}
}

func (c *RiskMutationControls) Receipts() *RiskMutationReceiptStore {
	if c == nil {
		return nil
	}
	return c.receipts
}

func (c *RiskMutationControls) Versions() *riskVersionCodec {
	if c == nil {
		return nil
	}
	return c.versions
}
