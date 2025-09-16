package deplconfig_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/cli/gram/api"
	"github.com/speakeasy-api/gram/server/cmd/cli/gram/deplconfig"
)

func TestSourceReader_ImplementsAssetSource(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "openapi.yaml")
	testContent := `openapi: 3.0.0
info:
  title: Example API
  version: 1.0.0
paths:
  /users:
    get:
      summary: Get users
      responses:
        '200':
          description: OK`

	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0600))

	source := deplconfig.Source{
		Type:     deplconfig.SourceTypeOpenAPIV3,
		Location: testFile,
		Name:     "Example API",
		Slug:     "example-api",
	}

	reader := deplconfig.NewSourceReader(source)

	// Verify it satisfies SourceReader interface by using it as one.
	var assetSource api.SourceReader = reader

	// Test all AssetSource methods work
	require.Equal(t, "openapiv3", assetSource.GetType())
	require.Equal(t, "application/yaml", assetSource.GetContentType())

	rc, size, err := assetSource.Read()
	require.NoError(t, err)
	require.Positive(t, size)
	defer func() {
		require.NoError(t, rc.Close())
	}()

	t.Logf("Successfully created AssetSource for file: %s (size: %d bytes)", testFile, size)
}

func TestSource_MissingRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  deplconfig.Source
		wantErr string
	}{
		{
			name: "missing name",
			source: deplconfig.Source{
				Type:     deplconfig.SourceTypeOpenAPIV3,
				Location: "/path/to/file",
				Name:     "",
				Slug:     "valid-slug",
			},
			wantErr: "source is missing required field 'name'",
		},
		{
			name: "missing slug",
			source: deplconfig.Source{
				Type:     deplconfig.SourceTypeOpenAPIV3,
				Location: "/path/to/file",
				Name:     "Valid Name",
				Slug:     "",
			},
			wantErr: "source is missing required field 'slug'",
		},
		{
			name: "missing both name and slug",
			source: deplconfig.Source{
				Type:     deplconfig.SourceTypeOpenAPIV3,
				Location: "/path/to/file",
				Name:     "",
				Slug:     "",
			},
			wantErr: "source is missing required field 'name'",
		},
		{
			name: "valid source",
			source: deplconfig.Source{
				Type:     deplconfig.SourceTypeOpenAPIV3,
				Location: "/path/to/file",
				Name:     "Valid Name",
				Slug:     "valid-slug",
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.source.MissingRequiredFields()

			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateUniqueNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sources []deplconfig.Source
		wantErr string
	}{
		{
			name: "unique names",
			sources: []deplconfig.Source{
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/one", Name: "API One", Slug: "api-one"},
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "api-two"},
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/three", Name: "API Three", Slug: "api-three"},
			},
			wantErr: "",
		},
		{
			name: "duplicate names",
			sources: []deplconfig.Source{
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/one", Name: "Duplicate API", Slug: "api-one"},
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "api-two"},
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/three", Name: "Duplicate API", Slug: "api-three"},
			},
			wantErr: "source names must be unique: ['Duplicate API' (2 times)]",
		},
		{
			name: "multiple duplicate names",
			sources: []deplconfig.Source{
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/one", Name: "First Duplicate", Slug: "api-one"},
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/two", Name: "Second Duplicate", Slug: "api-two"},
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/three", Name: "First Duplicate", Slug: "api-three"},
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/four", Name: "Second Duplicate", Slug: "api-four"},
			},
			wantErr: "source names must be unique:",
		},
		{
			name:    "empty sources",
			sources: []deplconfig.Source{},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := deplconfig.ValidateUniqueNames(tt.sources)

			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateUniqueSlugs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sources []deplconfig.Source
		wantErr string
	}{
		{
			name: "unique slugs",
			sources: []deplconfig.Source{
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/one", Name: "API One", Slug: "api-one"},
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "api-two"},
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/three", Name: "API Three", Slug: "api-three"},
			},
			wantErr: "",
		},
		{
			name: "duplicate slugs",
			sources: []deplconfig.Source{
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/one", Name: "API One", Slug: "duplicate-slug"},
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "api-two"},
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/three", Name: "API Three", Slug: "duplicate-slug"},
			},
			wantErr: "source slugs must be unique: ['duplicate-slug' (2 times)]",
		},
		{
			name: "multiple duplicate slugs",
			sources: []deplconfig.Source{
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/one", Name: "API One", Slug: "first-duplicate"},
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "second-duplicate"},
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/three", Name: "API Three", Slug: "first-duplicate"},
				{Type: deplconfig.SourceTypeOpenAPIV3, Location: "/path/four", Name: "API Four", Slug: "second-duplicate"},
			},
			wantErr: "source slugs must be unique:",
		},
		{
			name:    "empty sources",
			sources: []deplconfig.Source{},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := deplconfig.ValidateUniqueSlugs(tt.sources)

			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}
