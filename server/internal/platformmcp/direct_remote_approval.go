package platformmcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/authz"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// DirectRemoteApprovalState reports whether existing Shadow MCP enforcement
// permits a registered user-supplied URL. It deliberately reflects the same
// policy and grant state used at runtime; a Platform MCP registration never
// creates an approval bypass.
type DirectRemoteApprovalState struct {
	EnforcementActive bool
	Approved          bool
}

// DirectRemoteApprovalTxChecker evaluates enforcement in the distribution
// transaction so the consulted policy state cannot predate the attachment it
// authorizes.
type DirectRemoteApprovalTxChecker interface {
	CheckDirectRemoteApprovalTx(context.Context, riskrepo.DBTX, string, uuid.UUID, string) (DirectRemoteApprovalState, error)
}

// PostgresDirectRemoteApprovals reads enabled Shadow MCP policies and their
// URL-scoped grants. It is a narrow adapter over the existing enforcement data,
// not a parallel approval system.
type PostgresDirectRemoteApprovals struct {
	db *pgxpool.Pool
}

func NewPostgresDirectRemoteApprovals(db *pgxpool.Pool) *PostgresDirectRemoteApprovals {
	return &PostgresDirectRemoteApprovals{db: db}
}

var _ DirectRemoteApprovalTxChecker = (*PostgresDirectRemoteApprovals)(nil)

func (c *PostgresDirectRemoteApprovals) CheckDirectRemoteApprovalTx(ctx context.Context, db riskrepo.DBTX, organizationID string, projectID uuid.UUID, remoteURL string) (DirectRemoteApprovalState, error) {
	if c == nil || c.db == nil || db == nil || organizationID == "" || projectID == uuid.Nil {
		return DirectRemoteApprovalState{}, ErrRegistrationUnavailable
	}
	inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(remoteURL)
	if !ok {
		return DirectRemoteApprovalState{}, fmt.Errorf("canonicalize direct remote URL for approval consult: %w", ErrDirectRemoteRejected)
	}

	policies, err := riskrepo.New(db).ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
	if err != nil {
		return DirectRemoteApprovalState{}, fmt.Errorf("list Shadow MCP policies for direct remote approval consult: %w", err)
	}

	state := DirectRemoteApprovalState{Approved: true}
	for _, policy := range policies {
		if policy.Action != "block" {
			continue
		}
		state.EnforcementActive = true

		// Legacy policies without an explicit disposition are deny-by-default,
		// matching the enforcement writer. An allow-all policy instead blocks
		// only URLs with an explicit block grant.
		allowAll := policy.ShadowMcpDisposition.Valid && policy.ShadowMcpDisposition.String == shadowmcp.DispositionAllowAll
		scope := authz.ScopeRiskPolicyBypass
		if allowAll {
			scope = authz.ScopeRiskPolicyBlock
		}
		grants, err := authz.ListGrantsForResource(ctx, db, authz.Resource{
			OrganizationID: organizationID,
			Scope:          scope,
			ResourceID:     policy.ID.String(),
		})
		if err != nil {
			return DirectRemoteApprovalState{}, fmt.Errorf("list Shadow MCP policy grants for direct remote approval consult: %w", err)
		}
		granted := false
		for _, grant := range grants {
			if grant.Selector[authz.SelectorKeyServerURL] == inventoryURL.CanonicalURL {
				granted = true
				break
			}
		}
		if granted == allowAll {
			state.Approved = false
		}
	}
	return state, nil
}
