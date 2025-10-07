package deploy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSource_MissingRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  Source
		wantErr string
	}{
		{
			name: "missing name",
			source: Source{
				Type:     SourceTypeOpenAPIV3,
				Location: "/path/to/file",
				Name:     "",
				Slug:     "valid-slug",
				Runtime:  "nodejs:22",
			},
			wantErr: "source is missing required field 'name'",
		},
		{
			name: "missing slug",
			source: Source{
				Type:     SourceTypeOpenAPIV3,
				Location: "/path/to/file",
				Name:     "Valid Name",
				Slug:     "",
				Runtime:  "nodejs:22",
			},
			wantErr: "source is missing required field 'slug'",
		},
		{
			name: "missing both name and slug",
			source: Source{
				Type:     SourceTypeOpenAPIV3,
				Location: "/path/to/file",
				Name:     "",
				Slug:     "",
				Runtime:  "nodejs:22",
			},
			wantErr: "source is missing required field 'name'",
		},
		{
			name: "missing runtime for function",
			source: Source{
				Type:     SourceTypeFunction,
				Location: "/path/to/file.zip",
				Name:     "Example function",
				Slug:     "example-function",
				Runtime:  "",
			},
			wantErr: "source of type 'function' is missing required field 'runtime'",
		},
		{
			name: "valid function",
			source: Source{
				Type:     SourceTypeFunction,
				Location: "/path/to/file.zip",
				Name:     "Example function",
				Slug:     "example-function",
				Runtime:  "nodejs:22",
			},
			wantErr: "",
		},
		{
			name: "valid openapiv3",
			source: Source{
				Type:     SourceTypeOpenAPIV3,
				Location: "/path/to/file",
				Name:     "Valid Name",
				Slug:     "valid-slug",
				Runtime:  "nodejs:22",
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.source.Validate()

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
		sources []Source
		wantErr string
	}{
		{
			name: "unique names",
			sources: []Source{
				{Type: SourceTypeOpenAPIV3, Location: "/path/one", Name: "API One", Slug: "api-one", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "api-two", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/three", Name: "API Three", Slug: "api-three", Runtime: "nodejs:22"},
			},
			wantErr: "",
		},
		{
			name: "duplicate names",
			sources: []Source{
				{Type: SourceTypeOpenAPIV3, Location: "/path/one", Name: "Duplicate API", Slug: "api-one", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "api-two", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/three", Name: "Duplicate API", Slug: "api-three", Runtime: "nodejs:22"},
			},
			wantErr: "source names must be unique: ['Duplicate API' (2 times)]",
		},
		{
			name: "multiple duplicate names",
			sources: []Source{
				{Type: SourceTypeOpenAPIV3, Location: "/path/one", Name: "First Duplicate", Slug: "api-one", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/two", Name: "Second Duplicate", Slug: "api-two", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/three", Name: "First Duplicate", Slug: "api-three", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/four", Name: "Second Duplicate", Slug: "api-four", Runtime: "nodejs:22"},
			},
			wantErr: "source names must be unique:",
		},
		{
			name:    "empty sources",
			sources: []Source{},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateUniqueNames(tt.sources)

			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}
func TestFunctionMissingRuntime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sources []Source
		wantErr string
	}{
		{
			name: "",
			sources: []Source{
				{Type: SourceTypeOpenAPIV3, Location: "/path/one", Name: "API One", Slug: "api-one", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "api-two", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/three", Name: "API Three", Slug: "api-three", Runtime: "nodejs:22"},
			},
			wantErr: "",
		},
		{
			name: "duplicate names",
			sources: []Source{
				{Type: SourceTypeOpenAPIV3, Location: "/path/one", Name: "Duplicate API", Slug: "api-one", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "api-two", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/three", Name: "Duplicate API", Slug: "api-three", Runtime: "nodejs:22"},
			},
			wantErr: "source names must be unique: ['Duplicate API' (2 times)]",
		},
		{
			name: "multiple duplicate names",
			sources: []Source{
				{Type: SourceTypeOpenAPIV3, Location: "/path/one", Name: "First Duplicate", Slug: "api-one", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/two", Name: "Second Duplicate", Slug: "api-two", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/three", Name: "First Duplicate", Slug: "api-three", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/four", Name: "Second Duplicate", Slug: "api-four", Runtime: "nodejs:22"},
			},
			wantErr: "source names must be unique:",
		},
		{
			name:    "empty sources",
			sources: []Source{},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateUniqueNames(tt.sources)

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
		sources []Source
		wantErr string
	}{
		{
			name: "unique slugs",
			sources: []Source{
				{Type: SourceTypeOpenAPIV3, Location: "/path/one", Name: "API One", Slug: "api-one", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "api-two", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/three", Name: "API Three", Slug: "api-three", Runtime: "nodejs:22"},
			},
			wantErr: "",
		},
		{
			name: "duplicate slugs",
			sources: []Source{
				{Type: SourceTypeOpenAPIV3, Location: "/path/one", Name: "API One", Slug: "duplicate-slug", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "api-two", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/three", Name: "API Three", Slug: "duplicate-slug", Runtime: "nodejs:22"},
			},
			wantErr: "source slugs must be unique: ['duplicate-slug' (2 times)]",
		},
		{
			name: "multiple duplicate slugs",
			sources: []Source{
				{Type: SourceTypeOpenAPIV3, Location: "/path/one", Name: "API One", Slug: "first-duplicate", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "second-duplicate", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/three", Name: "API Three", Slug: "first-duplicate", Runtime: "nodejs:22"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/four", Name: "API Four", Slug: "second-duplicate", Runtime: "nodejs:22"},
			},
			wantErr: "source slugs must be unique:",
		},
		{
			name:    "empty sources",
			sources: []Source{},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateUniqueSlugs(tt.sources)

			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}
