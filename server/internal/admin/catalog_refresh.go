package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const CatalogRefreshPath = "/admin/catalog.refresh"

// CatalogRefreshHandler exposes exact-plan refresh only to live staff sessions
// in nonproduction. Source authority comes from startup configuration, never the
// request. Admin MCP writes require their own browser-approved proposals and are
// deliberately not routed through this cookie-authenticated internal endpoint.
func (s *Service) CatalogRefreshHandler(environment, sourceIdentity string, source CatalogSource, store *mcpregistry.CatalogAssetStore) http.Handler {
	handler := oops.ErrHandle(s.logger, func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			return oops.C(oops.CodeBadRequest)
		}
		ctx, err := s.authorizeAdminRequest(r)
		if err != nil {
			return err
		}
		if environment != "local" && environment != "dev" {
			return oops.E(oops.CodeForbidden, nil, "catalog refresh is unavailable in this deployment")
		}
		if source == nil || store == nil || sourceIdentity == "" {
			return oops.E(oops.CodeUnavailable, nil, "catalog refresh is not configured")
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			return oops.E(oops.CodeBadRequest, nil, "catalog refresh requires application/json")
		}
		var request struct {
			Action string `json:"action"`
			PlanID string `json:"planId"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			return oops.E(oops.CodeBadRequest, nil, "invalid catalog refresh request")
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			return oops.E(oops.CodeBadRequest, nil, "unexpected catalog refresh request data")
		}
		var planID uuid.UUID
		switch request.Action {
		case "preview", "status":
			if request.PlanID != "" {
				return oops.E(oops.CodeBadRequest, nil, "planId is accepted only for apply")
			}
		case "apply":
			planID, err = uuid.Parse(request.PlanID)
			if err != nil || planID == uuid.Nil {
				return oops.E(oops.CodeBadRequest, nil, "apply requires the exact preview planId")
			}
		default:
			return oops.E(oops.CodeBadRequest, nil, "action must be preview, apply, or status")
		}
		staff, ok := contextvalues.GetAdminAuthContext(ctx)
		if !ok || staff == nil || staff.OIDCSubject == "" {
			return oops.C(oops.CodeUnauthorized)
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		destinationAssets := *store
		destinationAssets.Operator = staff.Email
		refresh := mcpregistry.NewCatalogRefresher(s.registry, environment, sourceIdentity, source, &destinationAssets)
		var result any
		switch request.Action {
		case "preview":
			result, err = refresh.Preview(ctx, staff.OIDCSubject)
		case "apply":
			result, err = refresh.Apply(ctx, staff.OIDCSubject, planID)
		case "status":
			result, err = refresh.Status(ctx)
		}
		actor, _, _ := adminActor(ctx)
		action := "catalog.refresh." + request.Action
		if err != nil {
			// Do not log source errors, raw catalog metadata, or credentials. The
			// durable status/receipt carries bounded actionable categories.
			s.logger.WarnContext(ctx, "catalog refresh request failed", attr.SlogAuditAction(action), attr.SlogAuthorizationActorID(actor.String()))
			switch {
			case errors.Is(err, mcpregistry.ErrNotFound):
				return oops.E(oops.CodeNotFound, nil, "catalog refresh plan not found")
			case errors.Is(err, mcpregistry.ErrConflict), errors.Is(err, mcpregistry.ErrEndpointStructureImmutable):
				return oops.E(oops.CodeConflict, nil, "catalog refresh plan is stale; preview again")
			default:
				return oops.E(oops.CodeUnexpected, nil, "catalog refresh failed; inspect sync status and preview again")
			}
		}
		// A completed request can contain unresolved conflicts. Do not label it
		// a successful sync; the response and receipt distinguish that outcome.
		s.logger.InfoContext(ctx, "catalog refresh request completed", attr.SlogAuditAction(action), attr.SlogAuthorizationActorID(actor.String()))
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			return oops.E(oops.CodeUnexpected, err, "encode catalog refresh result")
		}
		return nil
	})
	return middleware.AdminOriginCheck(s.allowedOrigins)(handler)
}
