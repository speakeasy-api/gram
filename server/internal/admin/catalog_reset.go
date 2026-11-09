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

const CatalogResetPath = "/admin/catalog.reset"

// CatalogResetHandler exposes exact-plan reset only to live staff sessions
// in nonproduction. Source authority comes from startup configuration, never the
// request. Admin MCP writes require their own browser-approved proposals and are
// deliberately not routed through this cookie-authenticated internal endpoint.
func (s *Service) CatalogResetHandler(environment, sourceIdentity, destination string, source CatalogSource, store *mcpregistry.CatalogAssetStore) http.Handler {
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
			return oops.E(oops.CodeForbidden, nil, "catalog reset is unavailable in this deployment")
		}
		if source == nil || store == nil || sourceIdentity == "" || destination == "" {
			return oops.E(oops.CodeUnavailable, nil, "catalog reset is not configured")
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			return oops.E(oops.CodeBadRequest, nil, "catalog reset requires application/json")
		}
		var request struct {
			Action       string          `json:"action"`
			PlanID       json.RawMessage `json:"planId"`
			Confirmation json.RawMessage `json:"confirmation"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			return oops.E(oops.CodeBadRequest, nil, "invalid catalog reset request")
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			return oops.E(oops.CodeBadRequest, nil, "unexpected catalog reset request data")
		}
		var planID uuid.UUID
		var confirmation string
		switch request.Action {
		case "preview", "status":
			if request.PlanID != nil || request.Confirmation != nil {
				return oops.E(oops.CodeBadRequest, nil, "planId and confirmation are accepted only for apply")
			}
		case "apply":
			var rawID string
			if json.Unmarshal(request.PlanID, &rawID) != nil {
				return oops.E(oops.CodeBadRequest, nil, "apply requires the exact preview planId")
			}
			if json.Unmarshal(request.Confirmation, &confirmation) != nil || confirmation == "" {
				return oops.E(oops.CodeBadRequest, nil, "apply requires the exact preview confirmation")
			}
			planID, err = uuid.Parse(rawID)
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
		reset := mcpregistry.NewCatalogResetter(s.registry, environment, sourceIdentity, destination, source, &destinationAssets)
		var result any
		switch request.Action {
		case "preview":
			result, err = reset.Preview(ctx, staff.OIDCSubject)
		case "apply":
			result, err = reset.Apply(ctx, staff.OIDCSubject, planID, confirmation)
		case "status":
			result, err = reset.Status(ctx)
		}
		actor, _, _ := adminActor(ctx)
		action := "catalog.reset." + request.Action
		if err != nil {
			// Do not log source errors, raw catalog metadata, or credentials. The
			// durable status/receipt carries bounded actionable categories.
			s.logger.WarnContext(ctx, "catalog reset request failed", attr.SlogAuditAction(action), attr.SlogAuthorizationActorID(actor.String()))
			switch {
			case errors.Is(err, mcpregistry.ErrNotFound):
				return oops.E(oops.CodeNotFound, nil, "catalog reset plan not found")
			case errors.Is(err, mcpregistry.ErrConflict), errors.Is(err, mcpregistry.ErrEndpointStructureImmutable):
				return oops.E(oops.CodeConflict, nil, "catalog reset plan or confirmation does not match; preview again")
			default:
				return oops.E(oops.CodeUnexpected, nil, "catalog reset failed; inspect sync status and preview again")
			}
		}
		// The durable receipt is authoritative for the applied reset.
		s.logger.InfoContext(ctx, "catalog reset request completed", attr.SlogAuditAction(action), attr.SlogAuthorizationActorID(actor.String()))
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			return oops.E(oops.CodeUnexpected, err, "encode catalog reset result")
		}
		return nil
	})
	return middleware.AdminOriginCheck(s.allowedOrigins)(handler)
}
