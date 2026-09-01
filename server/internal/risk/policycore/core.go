package policycore

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// ErrLoadPolicy identifies a failure while loading the policy row, before
// audience or progress enrichment. Existing transports use it to preserve their
// established not-found mapping.
var ErrLoadPolicy = errors.New("load risk policy")

// Core provides transport-neutral policy reads and projections. Authorization
// remains the responsibility of the calling service.
type Core struct {
	db        repo.DBTX
	queries   *repo.Queries
	mutations *MutationDependencies
}

func New(db repo.DBTX, mutations ...MutationDependencies) *Core {
	core := &Core{db: db, queries: repo.New(db), mutations: nil}
	if len(mutations) > 0 {
		core.mutations = &mutations[0]
	}
	return core
}

// PageCursor identifies one policy in deterministic keyset order.
type PageCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// List returns all policies for a project with their audiences. Progress is
// intentionally omitted to avoid aggregate queries on list paths.
func (c *Core) List(ctx context.Context, organizationID string, projectID uuid.UUID) ([]Policy, error) {
	rows, err := c.queries.ListRiskPolicies(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list risk policies: %w", err)
	}
	if len(rows) == 0 {
		return []Policy{}, nil
	}

	policyIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		policyIDs = append(policyIDs, row.ID.String())
	}
	audienceByPolicy, err := c.audienceURNsByPolicy(ctx, organizationID, policyIDs)
	if err != nil {
		return nil, fmt.Errorf("load risk policy audiences: %w", err)
	}

	policies := make([]Policy, 0, len(rows))
	for _, row := range rows {
		policies = append(policies, Project(row, audienceByPolicy[row.ID.String()], nil))
	}
	return policies, nil
}

// ListPage returns at most limit+1 policies in deterministic keyset order. The
// extra row lets a transport decide whether to issue a next cursor without a
// separate count query.
func (c *Core) ListPage(ctx context.Context, organizationID string, projectID uuid.UUID, cursor *PageCursor, limit int32) ([]Policy, error) {
	params := repo.ListRiskPoliciesPageParams{
		ProjectID:       projectID,
		CursorCreatedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		CursorID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		PageLimit:       limit + 1,
	}
	if cursor != nil {
		params.CursorCreatedAt = pgtype.Timestamptz{Time: cursor.CreatedAt, InfinityModifier: pgtype.Finite, Valid: true}
		params.CursorID = uuid.NullUUID{UUID: cursor.ID, Valid: true}
	}
	rows, err := c.queries.ListRiskPoliciesPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list risk policies page: %w", err)
	}
	if len(rows) == 0 {
		return []Policy{}, nil
	}
	policyIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		policyIDs = append(policyIDs, row.ID.String())
	}
	audienceByPolicy, err := c.audienceURNsByPolicy(ctx, organizationID, policyIDs)
	if err != nil {
		return nil, fmt.Errorf("load risk policy page audiences: %w", err)
	}
	policies := make([]Policy, 0, len(rows))
	for _, row := range rows {
		policies = append(policies, Project(row, audienceByPolicy[row.ID.String()], nil))
	}
	return policies, nil
}

// Get returns one policy with best-effort message-analysis progress.
func (c *Core) Get(ctx context.Context, projectID, policyID uuid.UUID) (Policy, error) {
	row, err := c.queries.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{ID: policyID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, fmt.Errorf("%w: %w", ErrLoadPolicy, err)
	}
	if err != nil {
		return Policy{}, fmt.Errorf("get risk policy row: %w", err)
	}
	return c.ProjectWithProgress(ctx, row)
}

// ProjectWithProgress enriches an already-loaded row with its audience and
// best-effort message-analysis progress.
func (c *Core) ProjectWithProgress(ctx context.Context, row repo.RiskPolicy) (Policy, error) {
	total, err := c.queries.CountTotalMessages(ctx, uuid.NullUUID{UUID: row.ProjectID, Valid: true})
	if err != nil {
		total = 0
	}
	analyzed, err := c.queries.CountAnalyzedMessages(ctx, repo.CountAnalyzedMessagesParams{
		ProjectID:         row.ProjectID,
		RiskPolicyID:      row.ID,
		RiskPolicyVersion: row.Version,
	})
	if err != nil {
		analyzed = 0
	}
	audience, err := c.AudiencePrincipalURNs(ctx, row.OrganizationID, row.ID.String())
	if err != nil {
		return Policy{}, fmt.Errorf("load risk policy audience: %w", err)
	}
	return Project(row, audience, &Progress{Total: total, Analyzed: analyzed}), nil
}

// AudiencePrincipalURNs returns the exact-selector audience for one policy,
// sorted and deduplicated.
func (c *Core) AudiencePrincipalURNs(ctx context.Context, organizationID, policyID string) ([]string, error) {
	return audiencePrincipalURNs(ctx, c.db, organizationID, policyID)
}

func audiencePrincipalURNs(ctx context.Context, db repo.DBTX, organizationID, policyID string) ([]string, error) {
	grants, err := authz.ListGrantsForResource(ctx, db, authz.Resource{
		OrganizationID: organizationID,
		Scope:          authz.ScopeRiskPolicyEvaluate,
		ResourceID:     policyID,
	})
	if err != nil {
		return nil, fmt.Errorf("list risk policy audience grants: %w", err)
	}

	principalURNs := make([]string, 0, len(grants))
	for _, grant := range grants {
		if maps.Equal(grant.Selector, authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID)) {
			principalURNs = append(principalURNs, grant.PrincipalUrn)
		}
	}
	slices.Sort(principalURNs)
	return slices.Compact(principalURNs), nil
}

func (c *Core) audienceURNsByPolicy(ctx context.Context, organizationID string, policyIDs []string) (map[string][]string, error) {
	grants, err := authz.ListGrantsForResourceIDs(ctx, c.db, organizationID, authz.ScopeRiskPolicyEvaluate, policyIDs)
	if err != nil {
		return nil, fmt.Errorf("list risk policy audience grants: %w", err)
	}

	byPolicy := make(map[string][]string)
	for _, grant := range grants {
		policyID := grant.Selector.ResourceID()
		if maps.Equal(grant.Selector, authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID)) {
			byPolicy[policyID] = append(byPolicy[policyID], grant.PrincipalUrn)
		}
	}
	for policyID, principalURNs := range byPolicy {
		slices.Sort(principalURNs)
		byPolicy[policyID] = slices.Compact(principalURNs)
	}
	return byPolicy, nil
}
