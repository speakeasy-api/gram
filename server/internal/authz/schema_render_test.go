package authz_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	s "github.com/speakeasy-api/gram/server/internal/authz/schema"
)

var updateSchemaView = flag.Bool("update-schema-view", false, "rewrite docs/rbac-schema.authz from the compiled default registry")

// TestSchemaViewGolden pins docs/rbac-schema.authz, the readable view of the
// compiled default registry, so it cannot drift from the declarations. After
// changing a declaration, regenerate it with
//
//	go test ./internal/authz -run TestSchemaViewGolden -update-schema-view
func TestSchemaViewGolden(t *testing.T) {
	t.Parallel()

	reg, err := authz.DefaultRegistry()
	require.NoError(t, err)
	got := s.Render(reg)

	path := filepath.Join("..", "..", "..", "docs", "rbac-schema.authz")
	if *updateSchemaView {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), got, "docs/rbac-schema.authz is stale; rerun with -update-schema-view")
}
