package otelpub

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

type recordIDKey struct{}

type tenantKey struct{}

// WithRecordID sets the id that records emitted with ctx are published with; records sharing an id collapse downstream.
func WithRecordID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, recordIDKey{}, id)
}

func recordIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(recordIDKey{}).(string)
	return id
}

type tenant struct {
	organizationID string
	projectID      string
}

// WithTenant sets the organization and project for records emitted outside an authenticated request.
func WithTenant(ctx context.Context, organizationID, projectID string) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenant{organizationID: organizationID, projectID: projectID})
}

func tenantFrom(ctx context.Context) (tenant, bool) {
	// An incomplete explicit tenant is refused rather than replaced by the request's.
	if t, ok := ctx.Value(tenantKey{}).(tenant); ok {
		return t, t.organizationID != "" && t.projectID != ""
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" || authCtx.ProjectID == nil {
		return tenant{organizationID: "", projectID: ""}, false
	}
	return tenant{organizationID: authCtx.ActiveOrganizationID, projectID: authCtx.ProjectID.String()}, true
}
