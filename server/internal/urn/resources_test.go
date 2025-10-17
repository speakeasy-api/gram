package urn_test

import (
	"database/sql/driver"
	"encoding/json"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestNewResource(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		kind    urn.ResourceKind
		source  string
		uri     string
		wantErr error
	}{
		{
			name:    "valid function resource with file URI",
			kind:    urn.ResourceKindFunction,
			source:  "my-functions",
			uri:     "file:///docs/api.md",
			wantErr: nil,
		},
		{
			name:    "valid function resource with http URI",
			kind:    urn.ResourceKindFunction,
			source:  "api-functions",
			uri:     "https://api.example.com/data",
			wantErr: nil,
		},
		{
			name:    "valid function resource with postgres URI",
			kind:    urn.ResourceKindFunction,
			source:  "db-functions",
			uri:     "postgres://database/customers/schema",
			wantErr: nil,
		},
		{
			name:    "valid resource with screen URI",
			kind:    urn.ResourceKindFunction,
			source:  "screen-functions",
			uri:     "screen://localhost/display1",
			wantErr: nil,
		},
		{
			name:    "valid with numbers",
			kind:    urn.ResourceKindFunction,
			source:  "source123",
			uri:     "file:///path/to/resource123.txt",
			wantErr: nil,
		},
		{
			name:    "valid with underscores and dashes in source",
			kind:    urn.ResourceKindFunction,
			source:  "my_source-v2",
			uri:     "file:///data/file.json",
			wantErr: nil,
		},
		{
			name:    "empty source",
			kind:    urn.ResourceKindFunction,
			source:  "",
			uri:     "file:///docs/api.md",
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "empty uri",
			kind:    urn.ResourceKindFunction,
			source:  "my-source",
			uri:     "",
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "invalid kind",
			kind:    urn.ResourceKind("invalid"),
			source:  "my-source",
			uri:     "file:///docs/api.md",
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "source too long",
			kind:    urn.ResourceKindFunction,
			source:  strings.Repeat("a", 129), // maxSegmentLength+1
			uri:     "file:///docs/api.md",
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "source with invalid characters",
			kind:    urn.ResourceKindFunction,
			source:  "my source!",
			uri:     "file:///docs/api.md",
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "source starting with dash",
			kind:    urn.ResourceKindFunction,
			source:  "-my-source",
			uri:     "file:///docs/api.md",
			wantErr: nil,
		},
		{
			name:    "source ending with underscore",
			kind:    urn.ResourceKindFunction,
			source:  "my-source_",
			uri:     "file:///docs/api.md",
			wantErr: nil,
		},
		{
			name:    "uri with special characters gets slugified",
			kind:    urn.ResourceKindFunction,
			source:  "my-source",
			uri:     "file:///path/to/My Document (v2).pdf",
			wantErr: nil,
		},
		{
			name:    "uri with query parameters",
			kind:    urn.ResourceKindFunction,
			source:  "my-source",
			uri:     "https://api.example.com/data?key=value&id=123",
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resource := urn.NewResource(tt.kind, tt.source, tt.uri)

			if tt.wantErr != nil {
				// If we expect an error, marshaling should fail
				_, err := resource.MarshalJSON()
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NotEmpty(t, resource.String())
				require.Equal(t, tt.kind, resource.Kind)
				require.Equal(t, tt.source, resource.Source)
				require.NotEmpty(t, resource.SlugifiedURI)

				// Validate through marshaling operations
				_, err := resource.MarshalJSON()
				require.NoError(t, err)
				_, err = resource.MarshalText()
				require.NoError(t, err)
				_, err = resource.Value()
				require.NoError(t, err)
			}
		})
	}
}

func TestResource_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		resource urn.Resource
		want     string
	}{
		{
			name:     "function resource with file URI",
			resource: urn.NewResource(urn.ResourceKindFunction, "my-source", "file:///docs/api.md"),
			want:     "resources:function:my-source:file-docs-api-md",
		},
		{
			name:     "function resource with https URI",
			resource: urn.NewResource(urn.ResourceKindFunction, "api-server", "https://api.example.com/data"),
			want:     "resources:function:api-server:https-api-example-com-data",
		},
		{
			name:     "function resource with postgres URI",
			resource: urn.NewResource(urn.ResourceKindFunction, "db-access", "postgres://database/customers"),
			want:     "resources:function:db-access:postgres-database-customers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.resource.String()
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResource_URISlugification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		uri         string
		wantSlugURI string
	}{
		{
			name:        "file URI with simple path",
			uri:         "file:///docs/api.md",
			wantSlugURI: "file-docs-api-md",
		},
		{
			name:        "file URI with nested path",
			uri:         "file:///project/src/main.rs",
			wantSlugURI: "file-project-src-main-rs",
		},
		{
			name:        "https URI with domain and path",
			uri:         "https://api.example.com/v1/users",
			wantSlugURI: "https-api-example-com-v1-users",
		},
		{
			name:        "postgres URI",
			uri:         "postgres://database/customers/schema",
			wantSlugURI: "postgres-database-customers-schema",
		},
		{
			name:        "screen URI",
			uri:         "screen://localhost/display1",
			wantSlugURI: "screen-localhost-display1",
		},
		{
			name:        "URI with special characters",
			uri:         "file:///path/My Document (v2).pdf",
			wantSlugURI: "file-path-my-document-v2-pdf",
		},
		{
			name:        "URI with spaces and dots",
			uri:         "file:///Users/john.doe/My Files/doc.txt",
			wantSlugURI: "file-users-john-doe-my-files-doc-txt",
		},
		{
			name:        "URI with query parameters",
			uri:         "https://api.example.com/data?key=value",
			wantSlugURI: "https-api-example-com-data-key-value",
		},
		{
			name:        "URI with multiple slashes",
			uri:         "file:///path//to///file.txt",
			wantSlugURI: "file-path-to-file-txt",
		},
		{
			name:        "URI with trailing slash",
			uri:         "https://example.com/api/",
			wantSlugURI: "https-example-com-api",
		},
		{
			name:        "URI with underscores",
			uri:         "file:///my_project/src_files/main_app.js",
			wantSlugURI: "file-my_project-src_files-main_app-js",
		},
		{
			name:        "simple invalid URI gets sanitized",
			uri:         "not a valid URI!@#",
			wantSlugURI: "not-a-valid-uri",
		},
		{
			name:        "very long URI gets truncated",
			uri:         "file:///" + strings.Repeat("a", 200),
			wantSlugURI: "file-" + strings.Repeat("a", 123), // Total should be 128
		},
		{
			name:        "https URI with query parameters",
			uri:         "https://api.example.com/data?version=v1&format=json",
			wantSlugURI: "https-api-example-com-data-version-v1-format-json",
		},
		{
			name:        "https URI with multiple query parameters",
			uri:         "https://api.example.com/users?limit=10&offset=20&sort=name",
			wantSlugURI: "https-api-example-com-users-limit-10-offset-20-sort-name",
		},
		{
			name:        "file URI without query parameters",
			uri:         "file:///docs/guide.pdf",
			wantSlugURI: "file-docs-guide-pdf",
		},
		{
			name:        "postgres URI with query parameters",
			uri:         "postgres://database/customers?sslmode=require&connect_timeout=10",
			wantSlugURI: "postgres-database-customers-sslmode-require-connect_timeout-10",
		},
		{
			name:        "URI with template syntax - curly braces",
			uri:         "https://api.example.com/users/{id}/posts/{postId}",
			wantSlugURI: "https-api-example-com-users-id-posts-postid",
		},
		{
			name:        "URI with multiple template parameters",
			uri:         "https://api.example.com/{version}/users/{id}",
			wantSlugURI: "https-api-example-com-version-users-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resource := urn.NewResource(urn.ResourceKindFunction, "test-source", tt.uri)
			require.Equal(t, tt.wantSlugURI, resource.SlugifiedURI)
			require.LessOrEqual(t, len(resource.SlugifiedURI), 128, "slugified URI should not exceed 128 characters")
		})
	}
}

func TestResource_MarshalJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		resource urn.Resource
		want     string
		wantErr  error
	}{
		{
			name:     "valid function resource",
			resource: urn.NewResource(urn.ResourceKindFunction, "my-source", "file:///docs/api.md"),
			want:     `"resources:function:my-source:file-docs-api-md"`,
			wantErr:  nil,
		},
		{
			name:     "valid https resource",
			resource: urn.NewResource(urn.ResourceKindFunction, "api-server", "https://api.example.com/data"),
			want:     `"resources:function:api-server:https-api-example-com-data"`,
			wantErr:  nil,
		},
		{
			name:     "invalid resource - empty source",
			resource: urn.NewResource(urn.ResourceKindFunction, "", "file:///docs/api.md"),
			want:     "",
			wantErr:  urn.ErrInvalid,
		},
		{
			name:     "invalid resource - empty URI",
			resource: urn.NewResource(urn.ResourceKindFunction, "my-source", ""),
			want:     "",
			wantErr:  urn.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.resource.MarshalJSON()
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, string(got))
		})
	}
}

func TestResource_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    urn.Resource
		wantErr bool
	}{
		{
			name:    "valid function resource with file URI",
			input:   `"resources:function:my-source:file-docs-api-md"`,
			want:    urn.NewResource(urn.ResourceKindFunction, "my-source", "file:///docs/api.md"),
			wantErr: false,
		},
		{
			name:    "valid function resource with https URI",
			input:   `"resources:function:api-server:https-api-example-com-data"`,
			want:    urn.NewResource(urn.ResourceKindFunction, "api-server", "https://api.example.com/data"),
			wantErr: false,
		},
		{
			name:    "invalid json",
			input:   `invalid json`,
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "non-string json",
			input:   `123`,
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "invalid resource string - wrong prefix",
			input:   `"invalid:resource:string"`,
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   `""`,
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "too few segments",
			input:   `"resources:function:my-source"`,
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "too many segments",
			input:   `"resources:function:my-source:uri-slug:extra"`,
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "invalid resource kind",
			input:   `"resources:invalid:my-source:uri-slug"`,
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "empty kind",
			input:   `"resources::my-source:uri-slug"`,
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "empty source",
			input:   `"resources:function::uri-slug"`,
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "empty slugified URI",
			input:   `"resources:function:my-source:"`,
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "source with invalid characters",
			input:   `"resources:function:my source:uri-slug"`,
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "slugified URI with invalid characters",
			input:   `"resources:function:my-source:uri slug"`,
			want:    urn.Resource{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got urn.Resource
			err := got.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want.Kind, got.Kind)
			require.Equal(t, tt.want.Source, got.Source)
			require.Equal(t, tt.want.SlugifiedURI, got.SlugifiedURI)
		})
	}
}

func TestResource_Scan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   any
		want    urn.Resource
		wantErr bool
	}{
		{
			name:    "string input",
			input:   "resources:function:my-source:file-docs-api-md",
			want:    urn.NewResource(urn.ResourceKindFunction, "my-source", "file:///docs/api.md"),
			wantErr: false,
		},
		{
			name:    "byte slice input",
			input:   []byte("resources:function:api-server:https-api-example-com-data"),
			want:    urn.NewResource(urn.ResourceKindFunction, "api-server", "https://api.example.com/data"),
			wantErr: false,
		},
		{
			name:    "nil input",
			input:   nil,
			want:    urn.Resource{},
			wantErr: false,
		},
		{
			name:    "unsupported type",
			input:   123,
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "invalid string",
			input:   "invalid:resource:string",
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "invalid characters",
			input:   "resources:function:my source:uri-slug",
			want:    urn.Resource{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got urn.Resource
			err := got.Scan(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.input != nil {
				require.Equal(t, tt.want.Kind, got.Kind)
				require.Equal(t, tt.want.Source, got.Source)
				require.Equal(t, tt.want.SlugifiedURI, got.SlugifiedURI)
			}
		})
	}
}

func TestResource_Value(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		resource urn.Resource
		want     driver.Value
		wantErr  bool
	}{
		{
			name:     "valid function resource",
			resource: urn.NewResource(urn.ResourceKindFunction, "my-source", "file:///docs/api.md"),
			want:     "resources:function:my-source:file-docs-api-md",
			wantErr:  false,
		},
		{
			name:     "valid https resource",
			resource: urn.NewResource(urn.ResourceKindFunction, "api-server", "https://api.example.com/data"),
			want:     "resources:function:api-server:https-api-example-com-data",
			wantErr:  false,
		},
		{
			name:     "invalid resource - empty source",
			resource: urn.NewResource(urn.ResourceKindFunction, "", "file:///docs/api.md"),
			want:     "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.resource.Value()
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResource_MarshalText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		resource urn.Resource
		want     []byte
		wantErr  bool
	}{
		{
			name:     "valid function resource",
			resource: urn.NewResource(urn.ResourceKindFunction, "my-source", "file:///docs/api.md"),
			want:     []byte("resources:function:my-source:file-docs-api-md"),
			wantErr:  false,
		},
		{
			name:     "valid postgres resource",
			resource: urn.NewResource(urn.ResourceKindFunction, "db-access", "postgres://database/customers"),
			want:     []byte("resources:function:db-access:postgres-database-customers"),
			wantErr:  false,
		},
		{
			name:     "invalid resource - empty URI",
			resource: urn.NewResource(urn.ResourceKindFunction, "my-source", ""),
			want:     nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.resource.MarshalText()
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResource_UnmarshalText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   []byte
		want    urn.Resource
		wantErr bool
	}{
		{
			name:    "valid function resource",
			input:   []byte("resources:function:my-source:file-docs-api-md"),
			want:    urn.NewResource(urn.ResourceKindFunction, "my-source", "file:///docs/api.md"),
			wantErr: false,
		},
		{
			name:    "valid postgres resource",
			input:   []byte("resources:function:db-access:postgres-database-customers"),
			want:    urn.NewResource(urn.ResourceKindFunction, "db-access", "postgres://database/customers"),
			wantErr: false,
		},
		{
			name:    "invalid resource string",
			input:   []byte("invalid:resource:string"),
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   []byte(""),
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "invalid characters",
			input:   []byte("resources:function:my source:uri-slug"),
			want:    urn.Resource{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got urn.Resource
			err := got.UnmarshalText(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want.Kind, got.Kind)
			require.Equal(t, tt.want.Source, got.Source)
			require.Equal(t, tt.want.SlugifiedURI, got.SlugifiedURI)
		})
	}
}

func TestResource_roundTrip(t *testing.T) {
	t.Parallel()
	original := urn.NewResource(urn.ResourceKindFunction, "api-server", "https://api.example.com/v2/users")

	// Test JSON round trip
	jsonData, err := json.Marshal(original)
	require.NoError(t, err)

	var fromJSON urn.Resource
	err = json.Unmarshal(jsonData, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.Kind, fromJSON.Kind)
	require.Equal(t, original.Source, fromJSON.Source)
	require.Equal(t, original.SlugifiedURI, fromJSON.SlugifiedURI)

	// Test text round trip
	textData, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.Resource
	err = fromText.UnmarshalText(textData)
	require.NoError(t, err)
	require.Equal(t, original.Kind, fromText.Kind)
	require.Equal(t, original.Source, fromText.Source)
	require.Equal(t, original.SlugifiedURI, fromText.SlugifiedURI)

	// Test database round trip
	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.Resource
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.Kind, fromDB.Kind)
	require.Equal(t, original.Source, fromDB.Source)
	require.Equal(t, original.SlugifiedURI, fromDB.SlugifiedURI)
}

func TestResource_edgeCases(t *testing.T) {
	t.Parallel()

	t.Run("single character segments", func(t *testing.T) {
		t.Parallel()
		resource := urn.NewResource(urn.ResourceKindFunction, "a", "file:///b")

		// Should be valid - test through marshaling
		_, err := resource.MarshalJSON()
		require.NoError(t, err)
	})

	t.Run("boundary slug patterns for source", func(t *testing.T) {
		t.Parallel()
		validCases := []string{
			"a",
			"a1",
			"a1b2c3",
			"abc-def",
			"abc_def",
			"abc-def-ghi",
			"abc_def_ghi",
			"a1-b2_c3",
			"-abc", // starts with dash
			"_abc", // starts with underscore
			"abc-", // ends with dash
			"abc_", // ends with underscore
		}

		for _, validCase := range validCases {
			t.Run("valid_"+validCase, func(t *testing.T) {
				t.Parallel()
				resource := urn.NewResource(urn.ResourceKindFunction, validCase, "file:///test")

				// Test validation through marshaling
				_, err := resource.MarshalJSON()
				require.NoError(t, err)
			})
		}

		invalidCases := []string{
			"AB",  // uppercase
			"a b", // space
			"a.b", // dot
			"a@b", // at symbol
		}

		for _, invalidCase := range invalidCases {
			t.Run("invalid_"+invalidCase, func(t *testing.T) {
				t.Parallel()
				resource := urn.NewResource(urn.ResourceKindFunction, invalidCase, "file:///test")

				// Test validation through marshaling - should fail
				_, err := resource.MarshalJSON()
				require.Error(t, err)
			})
		}
	})

	t.Run("various URI schemes", func(t *testing.T) {
		t.Parallel()
		schemes := []struct {
			uri         string
			description string
		}{
			{"file:///path/to/file", "file scheme"},
			{"https://example.com/data", "https scheme"},
			{"http://example.com/api", "http scheme"},
			{"postgres://db/table", "postgres scheme"},
			{"mysql://db/table", "mysql scheme"},
			{"redis://localhost/key", "redis scheme"},
			{"mongodb://db/collection", "mongodb scheme"},
			{"ftp://server/file", "ftp scheme"},
			{"ssh://server/path", "ssh scheme"},
			{"screen://display", "screen scheme"},
		}

		for _, tc := range schemes {
			t.Run(tc.description, func(t *testing.T) {
				t.Parallel()
				resource := urn.NewResource(urn.ResourceKindFunction, "test-source", tc.uri)

				_, err := resource.MarshalJSON()
				require.NoError(t, err)
				require.NotEmpty(t, resource.SlugifiedURI)
			})
		}
	})

	t.Run("URI without scheme", func(t *testing.T) {
		t.Parallel()
		resource := urn.NewResource(urn.ResourceKindFunction, "test-source", "/just/a/path")

		_, err := resource.MarshalJSON()
		require.NoError(t, err)
		require.NotEmpty(t, resource.SlugifiedURI)
	})

	t.Run("URI with port", func(t *testing.T) {
		t.Parallel()
		resource := urn.NewResource(urn.ResourceKindFunction, "test-source", "https://example.com:8080/api")

		_, err := resource.MarshalJSON()
		require.NoError(t, err)
		require.Contains(t, resource.SlugifiedURI, "8080")
	})
}

func TestResource_validationCaching(t *testing.T) {
	t.Parallel()

	// Test that validation results are consistent across multiple calls
	resource := urn.NewResource(urn.ResourceKindFunction, "my-source", "file:///docs/api.md")

	// Multiple calls to operations that trigger validation should be consistent
	str1 := resource.String()
	str2 := resource.String()
	require.Equal(t, str1, str2)
	require.NotEmpty(t, str1)

	json1, err1 := resource.MarshalJSON()
	require.NoError(t, err1)
	json2, err2 := resource.MarshalJSON()
	require.NoError(t, err2)
	require.JSONEq(t, string(json1), string(json2))

	// Test with invalid resource
	invalidResource := urn.NewResource(urn.ResourceKindFunction, "", "file:///docs/api.md")

	_, err1 = invalidResource.MarshalJSON()
	require.Error(t, err1)
	_, err2 = invalidResource.MarshalJSON()
	require.Error(t, err2)
	// Error messages should be consistent
	require.Equal(t, err1.Error(), err2.Error())
}

func TestResource_IsZero(t *testing.T) {
	t.Parallel()

	t.Run("zero value resource", func(t *testing.T) {
		t.Parallel()
		var r urn.Resource
		require.True(t, r.IsZero())
	})

	t.Run("non-zero resource", func(t *testing.T) {
		t.Parallel()
		r := urn.NewResource(urn.ResourceKindFunction, "my-source", "file:///docs/api.md")
		require.False(t, r.IsZero())
	})

	t.Run("partially filled resource", func(t *testing.T) {
		t.Parallel()
		r := urn.Resource{
			Kind:   urn.ResourceKindFunction,
			Source: "my-source",
			// SlugifiedURI is empty
		}
		require.False(t, r.IsZero())
	})
}

func TestParseResource(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    urn.Resource
		wantErr bool
	}{
		{
			name:    "valid resource with file URI",
			input:   "resources:function:my-source:file-docs-api-md",
			want:    urn.NewResource(urn.ResourceKindFunction, "my-source", "file:///docs/api.md"),
			wantErr: false,
		},
		{
			name:    "valid resource with https URI",
			input:   "resources:function:api-server:https-api-example-com-data",
			want:    urn.NewResource(urn.ResourceKindFunction, "api-server", "https://api.example.com/data"),
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "too few segments",
			input:   "resources:function:my-source",
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "wrong prefix",
			input:   "tools:function:my-source:uri-slug",
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "invalid resource kind",
			input:   "resources:invalid:my-source:uri-slug",
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "empty segments",
			input:   "resources:function::uri-slug",
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "segment too long",
			input:   "resources:function:" + strings.Repeat("a", 129) + ":uri-slug",
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "invalid characters in source",
			input:   "resources:function:my source:uri-slug",
			want:    urn.Resource{},
			wantErr: true,
		},
		{
			name:    "invalid characters in slugified URI",
			input:   "resources:function:my-source:uri slug",
			want:    urn.Resource{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := urn.ParseResource(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want.Kind, got.Kind)
			require.Equal(t, tt.want.Source, got.Source)
			require.Equal(t, tt.want.SlugifiedURI, got.SlugifiedURI)
		})
	}
}
