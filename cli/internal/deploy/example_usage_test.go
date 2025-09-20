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
			},
			wantErr: "source is missing required field 'name'",
		},
		{
			name: "valid source",
			source: Source{
				Type:     SourceTypeOpenAPIV3,
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
				{Type: SourceTypeOpenAPIV3, Location: "/path/one", Name: "API One", Slug: "api-one"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "api-two"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/three", Name: "API Three", Slug: "api-three"},
			},
			wantErr: "",
		},
		{
			name: "duplicate names",
			sources: []Source{
				{Type: SourceTypeOpenAPIV3, Location: "/path/one", Name: "Duplicate API", Slug: "api-one"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "api-two"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/three", Name: "Duplicate API", Slug: "api-three"},
			},
			wantErr: "source names must be unique: ['Duplicate API' (2 times)]",
		},
		{
			name: "multiple duplicate names",
			sources: []Source{
				{Type: SourceTypeOpenAPIV3, Location: "/path/one", Name: "First Duplicate", Slug: "api-one"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/two", Name: "Second Duplicate", Slug: "api-two"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/three", Name: "First Duplicate", Slug: "api-three"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/four", Name: "Second Duplicate", Slug: "api-four"},
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
				{Type: SourceTypeOpenAPIV3, Location: "/path/one", Name: "API One", Slug: "api-one"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "api-two"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/three", Name: "API Three", Slug: "api-three"},
			},
			wantErr: "",
		},
		{
			name: "duplicate slugs",
			sources: []Source{
				{Type: SourceTypeOpenAPIV3, Location: "/path/one", Name: "API One", Slug: "duplicate-slug"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "api-two"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/three", Name: "API Three", Slug: "duplicate-slug"},
			},
			wantErr: "source slugs must be unique: ['duplicate-slug' (2 times)]",
		},
		{
			name: "multiple duplicate slugs",
			sources: []Source{
				{Type: SourceTypeOpenAPIV3, Location: "/path/one", Name: "API One", Slug: "first-duplicate"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/two", Name: "API Two", Slug: "second-duplicate"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/three", Name: "API Three", Slug: "first-duplicate"},
				{Type: SourceTypeOpenAPIV3, Location: "/path/four", Name: "API Four", Slug: "second-duplicate"},
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
