package remotesessions

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// emaService accumulates one vendor's bindings before the view is built.
type emaService struct {
	vendor      string
	name        string
	issuerID    uuid.UUID
	issuerIsOrg bool
	servers     int
	ready       int
}

// emaBindingSetUp reports whether a binding state means identity chaining was
// turned on for the server, whatever happened after.
func emaBindingSetUp(state string) bool {
	return state != "" && state != "unlinked" && state != "configuration_required"
}

func (s *Service) ListEMAServices(ctx context.Context, _ *orgissuersgen.ListEMAServicesPayload) (*orgissuersgen.ListEMAServicesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	org := authCtx.ActiveOrganizationID
	logger := s.logger.With(attr.SlogOrganizationID(org))
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: org, Dimensions: nil}); err != nil {
		return nil, err
	}
	q := repo.New(s.db)

	bindings, err := q.ListEMAServiceBindings(ctx, org)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list identity chaining bindings").LogError(ctx, logger)
	}
	services := map[string]*emaService{}
	for _, b := range bindings {
		vendor, err := NormalizeVendorIssuer(b.Issuer)
		if err != nil {
			continue
		}
		svc := services[vendor]
		if svc == nil {
			svc = &emaService{vendor: vendor, name: "", issuerID: uuid.Nil, issuerIsOrg: false, servers: 0, ready: 0}
			services[vendor] = svc
		}
		// An organization-level issuer record is the one setEMAProviders
		// should name; any record will do when there is none.
		if svc.issuerID == uuid.Nil || (b.IssuerIsOrganizationLevel && !svc.issuerIsOrg) {
			svc.issuerID, svc.issuerIsOrg = b.RemoteSessionIssuerID, b.IssuerIsOrganizationLevel
		}
		if svc.name == "" {
			svc.name = b.Name
		}
		if emaBindingSetUp(b.State) {
			svc.servers++
		}
		if b.State == "ready" {
			svc.ready++
		}
	}
	saved, err := q.ListEMAProviderVendors(ctx, org)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ema provider orders").LogError(ctx, logger)
	}
	for _, vendor := range saved {
		if services[vendor] == nil {
			services[vendor] = &emaService{vendor: vendor, name: "", issuerID: uuid.Nil, issuerIsOrg: false, servers: 0, ready: 0}
		}
	}

	acceptedRows, err := q.ListEMACredentialLastAcceptedByOrganization(ctx, org)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ema credential last accepted").LogError(ctx, logger)
	}
	accepted := map[string]map[EMAProviderKind]time.Time{}
	for _, row := range acceptedRows {
		vendor, err := NormalizeVendorIssuer(row.Issuer)
		if err != nil || !row.LastAcceptedAt.Valid {
			continue
		}
		byKind := accepted[vendor]
		if byKind == nil {
			byKind = map[EMAProviderKind]time.Time{}
			accepted[vendor] = byKind
		}
		kind := EMAProviderKind(row.EmaProvider)
		if at, ok := byKind[kind]; !ok || row.LastAcceptedAt.Time.After(at) {
			byKind[kind] = row.LastAcceptedAt.Time
		}
	}

	vendors := make([]string, 0, len(services))
	for vendor := range services {
		vendors = append(vendors, vendor)
	}
	sort.Strings(vendors)
	scope := EMAProviderScope{Endpoint: false, TrustedIssuer: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, SpeakeasySigns: s.speakeasySigningAvailable}
	items := make([]*orgissuersgen.EMAService, 0, len(vendors))
	for _, vendor := range vendors {
		svc := services[vendor]
		order, err := ResolveEMAProviders(ctx, q, org, vendor, scope)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "read ema provider order").LogError(ctx, logger)
		}
		order = StampEMAProviderLastAccepted(order, accepted[vendor])
		item := &orgissuersgen.EMAService{
			VendorIssuer:          vendor,
			Name:                  svc.name,
			RemoteSessionIssuerID: nil,
			EmaProviders:          s.liveEMAProvidersView(ctx, logger, org, order),
			ServerCount:           svc.servers,
			ReadyServerCount:      svc.ready,
		}
		if svc.issuerID != uuid.Nil {
			item.RemoteSessionIssuerID = conv.PtrEmpty(svc.issuerID.String())
		}
		items = append(items, item)
	}
	return &orgissuersgen.ListEMAServicesResult{Items: items}, nil
}
