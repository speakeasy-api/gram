package access

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	accessserver "github.com/speakeasy-api/gram/server/gen/http/access/server"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// The dashboard's permissions matrix tests run against the catalogue as the
// server serves it, so a scope the role endpoints refuse (risk policy grants
// belong to the risk policy surface) cannot come back as an editable row. The
// fixture is the wire form of access.listScopes; regenerate it with
// `go test ./internal/access -run TestListScopes_DashboardFixture -args -update`
// whenever the catalogue changes.
func TestListScopes_DashboardFixture(t *testing.T) {
	t.Parallel()

	body, err := json.Marshal(accessserver.NewListScopesResponseBody(scopeCatalogueResult()))
	require.NoError(t, err)
	testenv.RequireJSONGolden(t, "../../../client/dashboard/src/pages/access/listScopes.fixture.json", body)
}
