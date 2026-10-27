package chat

import (
	"encoding/json"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// HandleModels proxies supported upstream models, not a grant to invoke them.
// Completion-time policy, billing, and provider availability still apply.
func (s *Service) HandleModels(w http.ResponseWriter, r *http.Request) error {
	ctx, _, _, err := s.authorizeCompletion(r)
	if err != nil {
		return err
	}
	client, ok := s.completionClient.(openrouter.ModelCatalogClient)
	if !ok {
		return oops.E(oops.CodeGatewayError, nil, "model catalog unavailable")
	}
	catalog, err := client.ListModels(ctx)
	if err != nil {
		return oops.E(oops.CodeGatewayError, err, "model catalog unavailable")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(catalog); err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode model catalog").LogError(ctx, s.logger)
	}
	return nil
}
