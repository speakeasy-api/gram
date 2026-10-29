package securityevent

import (
	"context"
	"fmt"

	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/audit/securityevent/repo"
)

// The readers take and return the audit package's generated types, so the
// audit log API and staff views serve both logs through one code path. The
// queries mirror their audit counterparts column for column, which is what
// makes these conversions compile; a drift between the two fails the build.

// List is ListAuditLogs over security_audit_logs.
func List(ctx context.Context, dbtx repo.DBTX, params auditrepo.ListAuditLogsParams) ([]auditrepo.ListAuditLogsRow, error) {
	rows, err := repo.New(dbtx).ListSecurityAuditLogs(ctx, repo.ListSecurityAuditLogsParams(params))
	if err != nil {
		return nil, fmt.Errorf("list security audit logs: %w", err)
	}
	out := make([]auditrepo.ListAuditLogsRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, auditrepo.ListAuditLogsRow(row))
	}
	return out, nil
}

// ActorFacets is ListAuditActorFacets over security_audit_logs.
func ActorFacets(ctx context.Context, dbtx repo.DBTX, params auditrepo.ListAuditActorFacetsParams) ([]auditrepo.ListAuditActorFacetsRow, error) {
	rows, err := repo.New(dbtx).ListSecurityActorFacets(ctx, repo.ListSecurityActorFacetsParams(params))
	if err != nil {
		return nil, fmt.Errorf("list security actor facets: %w", err)
	}
	out := make([]auditrepo.ListAuditActorFacetsRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, auditrepo.ListAuditActorFacetsRow(row))
	}
	return out, nil
}

// SurfaceFacets is ListAuditSurfaceFacets over security_audit_logs.
func SurfaceFacets(ctx context.Context, dbtx repo.DBTX, params auditrepo.ListAuditSurfaceFacetsParams) ([]auditrepo.ListAuditSurfaceFacetsRow, error) {
	rows, err := repo.New(dbtx).ListSecuritySurfaceFacets(ctx, repo.ListSecuritySurfaceFacetsParams(params))
	if err != nil {
		return nil, fmt.Errorf("list security surface facets: %w", err)
	}
	out := make([]auditrepo.ListAuditSurfaceFacetsRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, auditrepo.ListAuditSurfaceFacetsRow(row))
	}
	return out, nil
}

// ActionFacets is ListAuditActionFacets over security_audit_logs.
func ActionFacets(ctx context.Context, dbtx repo.DBTX, params auditrepo.ListAuditActionFacetsParams) ([]auditrepo.ListAuditActionFacetsRow, error) {
	rows, err := repo.New(dbtx).ListSecurityActionFacets(ctx, repo.ListSecurityActionFacetsParams(params))
	if err != nil {
		return nil, fmt.Errorf("list security action facets: %w", err)
	}
	out := make([]auditrepo.ListAuditActionFacetsRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, auditrepo.ListAuditActionFacetsRow(row))
	}
	return out, nil
}
