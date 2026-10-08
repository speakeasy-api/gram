// Command gen-workload-catalog-fixture writes the workload platform catalog and
// the custom flows, exactly as workloadIdentities.listPlatforms and
// workloadIdentities.getCustomFlows serve them, to the dashboard's test
// fixtures, so dashboard tests render what the server serves.
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	srv "github.com/speakeasy-api/gram/server/gen/http/workload_identities/server"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy/catalog"
)

// The fixture paths are relative to server/internal/workloadpolicy/catalog,
// the package whose generate directive runs this command.
const (
	// catalogFixturePath is the listPlatforms response.
	catalogFixturePath = "../../../../client/dashboard/src/pages/workload-identities/setup/catalog.gen.json"

	// customFlowsFixturePath is the getCustomFlows response.
	customFlowsFixturePath = "../../../../client/dashboard/src/pages/workload-identities/custom/custom.gen.json"
)

func main() {
	source := catalog.Embedded()

	platforms, err := source.Platforms(context.Background())
	if err != nil {
		log.Fatalf("load catalog: %v", err)
	}
	writeFixture(catalogFixturePath, srv.NewListPlatformsResponseBody(mv.BuildWorkloadPlatformCatalogView(platforms)))

	flows, err := source.CustomFlows(context.Background())
	if err != nil {
		log.Fatalf("load custom flows: %v", err)
	}
	writeFixture(customFlowsFixturePath, srv.NewGetCustomFlowsResponseBody(mv.BuildWorkloadCustomFlowsView(flows)))
}

func writeFixture(path string, body any) {
	encoded, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		log.Fatalf("encode %s: %v", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		log.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
}
