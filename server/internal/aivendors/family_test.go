package aivendors_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

func TestFamilyID(t *testing.T) {
	t.Parallel()

	families := map[string]string{}
	for _, product := range aivendors.Products() {
		families[product.ID] = product.FamilyID()
	}

	tests := []struct {
		name    string
		product string
		family  string
	}{
		{name: "it groups claude code into the claude family", product: "claude-code", family: "claude"},
		{name: "it groups claude chat into the claude family", product: "claude", family: "claude"},
		{name: "it groups cowork into the claude family", product: "cowork", family: "claude"},
		{name: "it makes a product with no family its own", product: "codex", family: "codex"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.family, families[tt.product])
		})
	}
}
