// Command gen-workload-catalog-fixture writes the workload platform catalog,
// exactly as workloadIdentities.listPlatforms serves it, to the dashboard's
// test fixture. Dashboard tests then render the real catalog entries instead
// of a hand-kept copy that can drift from them.
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	srv "github.com/speakeasy-api/gram/server/gen/http/workload_identities/server"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy/catalog"
)

// fixturePath is relative to server/internal/workloadpolicy/catalog, the
// package whose generate directive runs this command.
const fixturePath = "../../../../client/dashboard/src/pages/workload-identities/setup/catalog.gen.json"

func main() {
	platforms, err := catalog.Embedded().Platforms(context.Background())
	if err != nil {
		log.Fatalf("load catalog: %v", err)
	}

	body := srv.NewListPlatformsResponseBody(mv.BuildWorkloadPlatformCatalogView(platforms))
	encoded, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		log.Fatalf("encode catalog: %v", err)
	}

	if err := os.WriteFile(fixturePath, append(encoded, '\n'), 0o600); err != nil {
		log.Fatalf("write fixture: %v", err)
	}
}
