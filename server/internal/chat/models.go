package chat

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type modelCatalogEntry struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

type modelCatalog struct {
	Object string              `json:"object"`
	Data   []modelCatalogEntry `json:"data"`
}

// HandleModels lists backend-supported models, not a grant to invoke them.
// Completion-time policy, billing, and provider availability still apply.
func (s *Service) HandleModels(w http.ResponseWriter, r *http.Request) error {
	ctx, _, _, err := s.authorizeCompletion(r)
	if err != nil {
		return err
	}

	ids := openrouter.AllowedModels()
	catalog := modelCatalog{Object: "list", Data: make([]modelCatalogEntry, 0, len(ids))}
	for _, id := range ids {
		provider, _, _ := strings.Cut(id, "/")
		catalog.Data = append(catalog.Data, modelCatalogEntry{ID: id, Object: "model", OwnedBy: provider})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(catalog); err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode model catalog").LogError(ctx, s.logger)
	}
	return nil
}
