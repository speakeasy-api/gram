package urn_test

import (
	"database/sql/driver"
	"encoding/json"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestNewFunctionRunner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		kind    urn.FunctionRunnerKind
		orgSlug string
		appName string
		wantErr error
	}{
		{
			name:    "valid local runner",
			kind:    urn.FunctionRunnerKindLocal,
			orgSlug: "my-org",
			appName: "my-app",
			wantErr: nil,
		},
		{
			name:    "valid fly runner",
			kind:    urn.FunctionRunnerKindFlyApp,
			orgSlug: "speakeasy",
			appName: "gram-worker",
			wantErr: nil,
		},
		{
			name:    "valid with numbers",
			kind:    urn.FunctionRunnerKindLocal,
			orgSlug: "org123",
			appName: "app456",
			wantErr: nil,
		},
		{
			name:    "valid with underscores and dashes",
			kind:    urn.FunctionRunnerKindFlyApp,
			orgSlug: "my_org-v2",
			appName: "my_app-name",
			wantErr: nil,
		},
		{
			name:    "empty org slug",
			kind:    urn.FunctionRunnerKindLocal,
			orgSlug: "",
			appName: "my-app",
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "empty app name",
			kind:    urn.FunctionRunnerKindLocal,
			orgSlug: "my-org",
			appName: "",
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "invalid kind",
			kind:    urn.FunctionRunnerKind("invalid"),
			orgSlug: "my-org",
			appName: "my-app",
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "org slug too long",
			kind:    urn.FunctionRunnerKindLocal,
			orgSlug: strings.Repeat("a", 129), // maxSegmentLength+1
			appName: "my-app",
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "app name too long",
			kind:    urn.FunctionRunnerKindLocal,
			orgSlug: "my-org",
			appName: strings.Repeat("a", 129), // maxSegmentLength+1
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "org slug with invalid characters",
			kind:    urn.FunctionRunnerKindLocal,
			orgSlug: "my org!",
			appName: "my-app",
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "app name with invalid characters",
			kind:    urn.FunctionRunnerKindLocal,
			orgSlug: "my-org",
			appName: "my app!",
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "org slug starting with dash",
			kind:    urn.FunctionRunnerKindLocal,
			orgSlug: "-my-org",
			appName: "my-app",
			wantErr: nil,
		},
		{
			name:    "app name ending with dash",
			kind:    urn.FunctionRunnerKindLocal,
			orgSlug: "my-org",
			appName: "my-app-",
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := urn.NewFunctionRunner(tt.kind, tt.orgSlug, tt.appName)

			if tt.wantErr != nil {
				// If we expect an error, the String method should return empty or we should get error when marshaling
				_, err := runner.MarshalJSON()
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NotEmpty(t, runner.String())
				require.Equal(t, tt.kind, runner.Kind)
				require.Equal(t, tt.orgSlug, runner.Tenancy)
				require.Equal(t, tt.appName, runner.Name)

				// Validate through marshaling operations
				_, err := runner.MarshalJSON()
				require.NoError(t, err)
				_, err = runner.MarshalText()
				require.NoError(t, err)
				_, err = runner.Value()
				require.NoError(t, err)
			}
		})
	}
}

func TestFunctionRunner_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		runner urn.FunctionRunner
		want   string
	}{
		{
			name:   "local runner",
			runner: urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, "my-org", "my-app"),
			want:   "gfr:local:my-org:my-app",
		},
		{
			name:   "fly runner",
			runner: urn.NewFunctionRunner(urn.FunctionRunnerKindFlyApp, "speakeasy", "gram-worker"),
			want:   "gfr:fly:speakeasy:gram-worker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.runner.String()
			require.Equal(t, tt.want, got)
		})
	}
}

func TestFunctionRunner_MarshalJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		runner  urn.FunctionRunner
		want    string
		wantErr error
	}{
		{
			name:    "valid local runner",
			runner:  urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, "my-org", "my-app"),
			want:    `"gfr:local:my-org:my-app"`,
			wantErr: nil,
		},
		{
			name:    "valid fly runner",
			runner:  urn.NewFunctionRunner(urn.FunctionRunnerKindFlyApp, "speakeasy", "gram-worker"),
			want:    `"gfr:fly:speakeasy:gram-worker"`,
			wantErr: nil,
		},
		{
			name:    "invalid runner - empty org",
			runner:  urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, "", "my-app"),
			want:    "",
			wantErr: urn.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.runner.MarshalJSON()
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, string(got))
		})
	}
}

func TestFunctionRunner_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    urn.FunctionRunner
		wantErr bool
	}{
		{
			name:    "valid local runner",
			input:   `"gfr:local:my-org:my-app"`,
			want:    urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, "my-org", "my-app"),
			wantErr: false,
		},
		{
			name:    "valid fly runner",
			input:   `"gfr:fly:speakeasy:gram-worker"`,
			want:    urn.NewFunctionRunner(urn.FunctionRunnerKindFlyApp, "speakeasy", "gram-worker"),
			wantErr: false,
		},
		{
			name:    "invalid json",
			input:   `invalid json`,
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "non-string json",
			input:   `123`,
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "invalid runner string - wrong prefix",
			input:   `"invalid:runner:string"`,
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   `""`,
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "too few segments",
			input:   `"gfr:local:my-org"`,
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "too many segments",
			input:   `"gfr:local:my-org:my-app:extra"`,
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "invalid runner kind",
			input:   `"gfr:invalid:my-org:my-app"`,
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "empty kind",
			input:   `"gfr::my-org:my-app"`,
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "empty org slug",
			input:   `"gfr:local::my-app"`,
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "empty app name",
			input:   `"gfr:local:my-org:"`,
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "org slug with invalid characters",
			input:   `"gfr:local:my org:my-app"`,
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "app name with invalid characters",
			input:   `"gfr:local:my-org:my app"`,
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got urn.FunctionRunner
			err := got.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want.Kind, got.Kind)
			require.Equal(t, tt.want.Tenancy, got.Tenancy)
			require.Equal(t, tt.want.Name, got.Name)
		})
	}
}

func TestFunctionRunner_Scan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   any
		want    urn.FunctionRunner
		wantErr bool
	}{
		{
			name:    "string input",
			input:   "gfr:local:my-org:my-app",
			want:    urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, "my-org", "my-app"),
			wantErr: false,
		},
		{
			name:    "byte slice input",
			input:   []byte("gfr:fly:speakeasy:gram-worker"),
			want:    urn.NewFunctionRunner(urn.FunctionRunnerKindFlyApp, "speakeasy", "gram-worker"),
			wantErr: false,
		},
		{
			name:    "nil input",
			input:   nil,
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: false,
		},
		{
			name:    "unsupported type",
			input:   123,
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "invalid string",
			input:   "invalid:runner:string",
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "invalid characters",
			input:   "gfr:local:my org:my-app",
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got urn.FunctionRunner
			err := got.Scan(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.input != nil {
				require.Equal(t, tt.want.Kind, got.Kind)
				require.Equal(t, tt.want.Tenancy, got.Tenancy)
				require.Equal(t, tt.want.Name, got.Name)
			}
		})
	}
}

func TestFunctionRunner_Value(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		runner  urn.FunctionRunner
		want    driver.Value
		wantErr bool
	}{
		{
			name:    "valid local runner",
			runner:  urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, "my-org", "my-app"),
			want:    "gfr:local:my-org:my-app",
			wantErr: false,
		},
		{
			name:    "valid fly runner",
			runner:  urn.NewFunctionRunner(urn.FunctionRunnerKindFlyApp, "speakeasy", "gram-worker"),
			want:    "gfr:fly:speakeasy:gram-worker",
			wantErr: false,
		},
		{
			name:    "invalid runner - empty org",
			runner:  urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, "", "my-app"),
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.runner.Value()
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestFunctionRunner_MarshalText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		runner  urn.FunctionRunner
		want    []byte
		wantErr bool
	}{
		{
			name:    "valid local runner",
			runner:  urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, "my-org", "my-app"),
			want:    []byte("gfr:local:my-org:my-app"),
			wantErr: false,
		},
		{
			name:    "valid fly runner",
			runner:  urn.NewFunctionRunner(urn.FunctionRunnerKindFlyApp, "speakeasy", "gram-worker"),
			want:    []byte("gfr:fly:speakeasy:gram-worker"),
			wantErr: false,
		},
		{
			name:    "invalid runner - empty name",
			runner:  urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, "my-org", ""),
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.runner.MarshalText()
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestFunctionRunner_UnmarshalText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   []byte
		want    urn.FunctionRunner
		wantErr bool
	}{
		{
			name:    "valid local runner",
			input:   []byte("gfr:local:my-org:my-app"),
			want:    urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, "my-org", "my-app"),
			wantErr: false,
		},
		{
			name:    "valid fly runner",
			input:   []byte("gfr:fly:speakeasy:gram-worker"),
			want:    urn.NewFunctionRunner(urn.FunctionRunnerKindFlyApp, "speakeasy", "gram-worker"),
			wantErr: false,
		},
		{
			name:    "invalid runner string",
			input:   []byte("invalid:runner:string"),
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   []byte(""),
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "invalid characters",
			input:   []byte("gfr:local:my org:my-app"),
			want:    urn.FunctionRunner{Kind: "", Tenancy: "", Name: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got urn.FunctionRunner
			err := got.UnmarshalText(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want.Kind, got.Kind)
			require.Equal(t, tt.want.Tenancy, got.Tenancy)
			require.Equal(t, tt.want.Name, got.Name)
		})
	}
}

func TestFunctionRunner_roundTrip(t *testing.T) {
	t.Parallel()
	original := urn.NewFunctionRunner(urn.FunctionRunnerKindFlyApp, "speakeasy", "gram-worker-v2")

	// Test JSON round trip
	jsonData, err := json.Marshal(original)
	require.NoError(t, err)

	var fromJSON urn.FunctionRunner
	err = json.Unmarshal(jsonData, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.Kind, fromJSON.Kind)
	require.Equal(t, original.Tenancy, fromJSON.Tenancy)
	require.Equal(t, original.Name, fromJSON.Name)

	// Test text round trip
	textData, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.FunctionRunner
	err = fromText.UnmarshalText(textData)
	require.NoError(t, err)
	require.Equal(t, original.Kind, fromText.Kind)
	require.Equal(t, original.Tenancy, fromText.Tenancy)
	require.Equal(t, original.Name, fromText.Name)

	// Test database round trip
	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.FunctionRunner
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.Kind, fromDB.Kind)
	require.Equal(t, original.Tenancy, fromDB.Tenancy)
	require.Equal(t, original.Name, fromDB.Name)
}

func TestFunctionRunner_edgeCases(t *testing.T) {
	t.Parallel()

	t.Run("single character segments", func(t *testing.T) {
		t.Parallel()
		runner := urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, "a", "b")

		// Should be valid - test through marshaling
		_, err := runner.MarshalJSON()
		require.NoError(t, err)
	})

	t.Run("boundary slug patterns", func(t *testing.T) {
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
				runner := urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, validCase, validCase)

				// Test validation through marshaling
				_, err := runner.MarshalJSON()
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
				runner := urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, invalidCase, "valid")

				// Test validation through marshaling - should fail
				_, err := runner.MarshalJSON()
				require.Error(t, err)
			})
		}
	})
}

func TestFunctionRunner_validationCaching(t *testing.T) {
	t.Parallel()

	// Test that validation results are consistent across multiple calls
	runner := urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, "my-org", "my-app")

	// Multiple calls to operations that trigger validation should be consistent
	str1 := runner.String()
	str2 := runner.String()
	require.Equal(t, str1, str2)
	require.NotEmpty(t, str1)

	json1, err1 := runner.MarshalJSON()
	require.NoError(t, err1)
	json2, err2 := runner.MarshalJSON()
	require.NoError(t, err2)
	require.JSONEq(t, string(json1), string(json2))

	// Test with invalid runner
	invalidRunner := urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, "", "my-app")

	_, err1 = invalidRunner.MarshalJSON()
	require.Error(t, err1)
	_, err2 = invalidRunner.MarshalJSON()
	require.Error(t, err2)
	// Error messages should be consistent
	require.Equal(t, err1.Error(), err2.Error())
}
