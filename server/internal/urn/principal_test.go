package urn_test

import (
	"database/sql/driver"
	"encoding/json"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestNewPrincipal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		typ     urn.PrincipalType
		id      string
		wantErr error
	}{
		{
			name:    "valid user principal",
			typ:     urn.PrincipalTypeUser,
			id:      "user_01abc",
			wantErr: nil,
		},
		{
			name:    "valid role principal",
			typ:     urn.PrincipalTypeRole,
			id:      "admin",
			wantErr: nil,
		},
		{
			name:    "valid role member",
			typ:     urn.PrincipalTypeRole,
			id:      "member",
			wantErr: nil,
		},
		{
			name:    "empty type",
			typ:     "",
			id:      "some-id",
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "unknown type",
			typ:     urn.PrincipalType("team"),
			id:      "backend",
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "empty id",
			typ:     urn.PrincipalTypeUser,
			id:      "",
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "id too long",
			typ:     urn.PrincipalTypeUser,
			id:      strings.Repeat("a", 129),
			wantErr: urn.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := urn.NewPrincipal(tt.typ, tt.id)

			if tt.wantErr != nil {
				_, err := p.MarshalJSON()
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NotEmpty(t, p.String())
				require.Equal(t, tt.typ, p.Type)
				require.Equal(t, tt.id, p.ID)

				_, err := p.MarshalJSON()
				require.NoError(t, err)
				_, err = p.MarshalText()
				require.NoError(t, err)
				_, err = p.Value()
				require.NoError(t, err)
			}
		})
	}
}

func TestPrincipal_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		principal urn.Principal
		want      string
	}{
		{
			name:      "user principal",
			principal: urn.NewPrincipal(urn.PrincipalTypeUser, "user_01abc"),
			want:      "user:user_01abc",
		},
		{
			name:      "role principal",
			principal: urn.NewPrincipal(urn.PrincipalTypeRole, "admin"),
			want:      "role:admin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tt.principal.String())
		})
	}
}

func TestParsePrincipal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    urn.Principal
		wantErr bool
	}{
		{
			name:    "valid user",
			input:   "user:user_01abc",
			want:    urn.NewPrincipal(urn.PrincipalTypeUser, "user_01abc"),
			wantErr: false,
		},
		{
			name:    "valid role",
			input:   "role:admin",
			want:    urn.NewPrincipal(urn.PrincipalTypeRole, "admin"),
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "no delimiter",
			input:   "userabc",
			wantErr: true,
		},
		{
			name:    "empty id after delimiter",
			input:   "user:",
			wantErr: true,
		},
		{
			name:    "unknown type",
			input:   "team:backend",
			wantErr: true,
		},
		{
			name:    "only delimiter",
			input:   ":",
			wantErr: true,
		},
		{
			name:    "empty type before delimiter",
			input:   ":some-id",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := urn.ParsePrincipal(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want.Type, got.Type)
			require.Equal(t, tt.want.ID, got.ID)
		})
	}
}

func TestPrincipal_MarshalJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		principal urn.Principal
		want      string
		wantErr   error
	}{
		{
			name:      "valid user",
			principal: urn.NewPrincipal(urn.PrincipalTypeUser, "user_01abc"),
			want:      `"user:user_01abc"`,
			wantErr:   nil,
		},
		{
			name:      "valid role",
			principal: urn.NewPrincipal(urn.PrincipalTypeRole, "admin"),
			want:      `"role:admin"`,
			wantErr:   nil,
		},
		{
			name:      "invalid - empty id",
			principal: urn.NewPrincipal(urn.PrincipalTypeUser, ""),
			want:      "",
			wantErr:   urn.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.principal.MarshalJSON()
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, string(got))
		})
	}
}

func TestPrincipal_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    urn.Principal
		wantErr bool
	}{
		{
			name:    "valid user",
			input:   `"user:user_01abc"`,
			want:    urn.NewPrincipal(urn.PrincipalTypeUser, "user_01abc"),
			wantErr: false,
		},
		{
			name:    "valid role",
			input:   `"role:admin"`,
			want:    urn.NewPrincipal(urn.PrincipalTypeRole, "admin"),
			wantErr: false,
		},
		{
			name:    "invalid json",
			input:   `invalid`,
			wantErr: true,
		},
		{
			name:    "non-string json",
			input:   `123`,
			wantErr: true,
		},
		{
			name:    "unknown type",
			input:   `"team:backend"`,
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   `""`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got urn.Principal
			err := got.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want.Type, got.Type)
			require.Equal(t, tt.want.ID, got.ID)
		})
	}
}

func TestPrincipal_Scan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   any
		want    urn.Principal
		wantErr bool
	}{
		{
			name:    "string input",
			input:   "user:user_01abc",
			want:    urn.NewPrincipal(urn.PrincipalTypeUser, "user_01abc"),
			wantErr: false,
		},
		{
			name:    "byte slice input",
			input:   []byte("role:admin"),
			want:    urn.NewPrincipal(urn.PrincipalTypeRole, "admin"),
			wantErr: false,
		},
		{
			name:    "nil input",
			input:   nil,
			want:    urn.Principal{},
			wantErr: false,
		},
		{
			name:    "unsupported type",
			input:   123,
			wantErr: true,
		},
		{
			name:    "invalid string",
			input:   "garbage",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got urn.Principal
			err := got.Scan(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.input != nil {
				require.Equal(t, tt.want.Type, got.Type)
				require.Equal(t, tt.want.ID, got.ID)
			}
		})
	}
}

func TestPrincipal_Value(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		principal urn.Principal
		want      driver.Value
		wantErr   bool
	}{
		{
			name:      "valid user",
			principal: urn.NewPrincipal(urn.PrincipalTypeUser, "user_01abc"),
			want:      "user:user_01abc",
			wantErr:   false,
		},
		{
			name:      "valid role",
			principal: urn.NewPrincipal(urn.PrincipalTypeRole, "admin"),
			want:      "role:admin",
			wantErr:   false,
		},
		{
			name:      "invalid - empty id",
			principal: urn.NewPrincipal(urn.PrincipalTypeUser, ""),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.principal.Value()
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestPrincipal_IsZero(t *testing.T) {
	t.Parallel()

	require.True(t, urn.Principal{}.IsZero())
	require.False(t, urn.NewPrincipal(urn.PrincipalTypeUser, "abc").IsZero())
}

func TestPrincipal_roundTrip(t *testing.T) {
	t.Parallel()
	original := urn.NewPrincipal(urn.PrincipalTypeUser, "user_01abc")

	// JSON round trip
	jsonData, err := json.Marshal(original)
	require.NoError(t, err)

	var fromJSON urn.Principal
	err = json.Unmarshal(jsonData, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.Type, fromJSON.Type)
	require.Equal(t, original.ID, fromJSON.ID)

	// Text round trip
	textData, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.Principal
	err = fromText.UnmarshalText(textData)
	require.NoError(t, err)
	require.Equal(t, original.Type, fromText.Type)
	require.Equal(t, original.ID, fromText.ID)

	// Database round trip
	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.Principal
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.Type, fromDB.Type)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestPrincipal_validationCaching(t *testing.T) {
	t.Parallel()

	p := urn.NewPrincipal(urn.PrincipalTypeRole, "admin")

	str1 := p.String()
	str2 := p.String()
	require.Equal(t, str1, str2)
	require.NotEmpty(t, str1)

	json1, err1 := p.MarshalJSON()
	require.NoError(t, err1)
	json2, err2 := p.MarshalJSON()
	require.NoError(t, err2)
	require.JSONEq(t, string(json1), string(json2))

	invalid := urn.NewPrincipal(urn.PrincipalTypeUser, "")
	_, err1 = invalid.MarshalJSON()
	require.Error(t, err1)
	_, err2 = invalid.MarshalJSON()
	require.Error(t, err2)
	require.Equal(t, err1.Error(), err2.Error())
}

func TestPrincipal_idPermissiveness(t *testing.T) {
	t.Parallel()

	// Principal IDs are intentionally permissive to accommodate external systems
	// (e.g. WorkOS user IDs like "user_01HXYZ..."). Unlike tool URNs, they do
	// not enforce the slug pattern.
	permissiveCases := []string{
		"user_01HXYZ",       // uppercase (WorkOS format)
		"user_01abc.def",    // dots
		"abc123",            // plain alphanumeric
		"a",                 // single char
		"some-long-role-id", // dashes
	}

	for _, id := range permissiveCases {
		t.Run("accepts_"+id, func(t *testing.T) {
			t.Parallel()
			p := urn.NewPrincipal(urn.PrincipalTypeUser, id)
			_, err := p.Value()
			require.NoError(t, err)
		})
	}
}
