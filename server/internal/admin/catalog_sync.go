package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const CatalogSyncPath = "/admin/catalog.sync"

// CatalogSource is a startup-configured authenticated production reader. Request
// input cannot choose a source, destination, credential, or deployment identity.
type CatalogSource interface {
	Fetch(context.Context) (mcpregistry.CatalogBundle, error)
}

// CatalogSyncHandler is an internal staff-only additive catch-up operation.
// Startup supplies a configured source identity to record trustworthy baselines;
// legacy callers without one retain additive-only import semantics. It is
// deliberately not an Admin MCP write tool.
func (s *Service) CatalogSyncHandler(environment string, source CatalogSource, store *mcpregistry.CatalogAssetStore, sourceIdentity ...string) http.Handler {
	handler := oops.ErrHandle(s.logger, func(w http.ResponseWriter, r *http.Request) error {
		if r.Method != http.MethodPost {
			return oops.C(oops.CodeBadRequest)
		}
		ctx, err := s.authorizeAdminRequest(r)
		if err != nil {
			return err
		}
		// Reject before decoding input, fetching source data, or touching assets.
		if environment != "local" && environment != "dev" {
			return oops.E(oops.CodeForbidden, nil, "catalog sync is unavailable in this deployment")
		}
		if source == nil || store == nil {
			return oops.E(oops.CodeUnavailable, nil, "catalog sync is not configured")
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			return oops.E(oops.CodeBadRequest, nil, "catalog sync requires application/json")
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		var request struct{}
		if err := decoder.Decode(&request); err != nil {
			return oops.E(oops.CodeBadRequest, err, "decode catalog sync request")
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			return oops.E(oops.CodeBadRequest, err, "unexpected catalog sync request data")
		}
		staff, ok := contextvalues.GetAdminAuthContext(ctx)
		if !ok || staff == nil || staff.OIDCSubject == "" {
			return oops.C(oops.CodeUnauthorized)
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		actor, _, _ := adminActor(ctx)
		destinationAssets := *store
		destinationAssets.Operator = staff.Email
		var result mcpregistry.CatalogImportResult
		if len(sourceIdentity) == 1 && sourceIdentity[0] != "" {
			result, err = mcpregistry.NewCatalogRefresher(s.registry, environment, sourceIdentity[0], source, &destinationAssets).Import(ctx, staff.OIDCSubject)
		} else {
			var bundle mcpregistry.CatalogBundle
			bundle, err = source.Fetch(ctx)
			if err == nil {
				result, err = mcpregistry.NewCatalogImporter(s.registry, environment, &destinationAssets).Import(ctx, bundle)
			}
		}
		if err != nil {
			s.logger.WarnContext(ctx, "catalog sync failed", attr.SlogAuditAction("catalog.sync"), attr.SlogAuthorizationActorID(actor.String()))
			return s.registryError(ctx, err)
		}
		s.logger.InfoContext(ctx, "catalog sync completed", attr.SlogAuditAction("catalog.sync"), attr.SlogAuthorizationActorID(actor.String()), attr.SlogCatalogTransferCreated(len(result.Created)), attr.SlogCatalogTransferConflicts(len(result.Conflicts)))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			return oops.E(oops.CodeUnexpected, err, "encode catalog sync result")
		}
		return nil
	})
	return middleware.AdminOriginCheck(s.allowedOrigins)(handler)
}
