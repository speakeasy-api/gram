package urn_test

import (
	"database/sql/driver"
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
			name:    "valid user",
			typ:     urn.PrincipalTypeUser,
			id:      "user_abc123",
			wantErr: nil,
		},
		{
			name:    "valid role",
			typ:     urn.PrincipalTypeRole,
			id:      "admin",
			wantErr: nil,
		},
		{
			name:    "empty type",
			typ:     "",
			id:      "user_abc",
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "unknown type",
			typ:     "team",
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
			p, err := urn.NewPrincipal(tt.typ, tt.id)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.typ, p.Type)
			require.Equal(t, tt.id, p.ID)
		})
	}
}

func TestParsePrincipal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantTyp urn.PrincipalType
		wantID  string
		wantErr bool
	}{
		{
			name:    "valid user",
			input:   "user:user_abc123",
			wantTyp: urn.PrincipalTypeUser,
			wantID:  "user_abc123",
			wantErr: false,
		},
		{
			name:    "valid role",
			input:   "role:admin",
			wantTyp: urn.PrincipalTypeRole,
			wantID:  "admin",
			wantErr: false,
		},
		{
			name:    "id containing colon",
			input:   "user:org:user_abc",
			wantTyp: urn.PrincipalTypeUser,
			wantID:  "org:user_abc",
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
			name:    "unknown type",
			input:   "team:backend",
			wantErr: true,
		},
		{
			name:    "empty id after colon",
			input:   "user:",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := urn.ParsePrincipal(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantTyp, p.Type)
			require.Equal(t, tt.wantID, p.ID)
		})
	}
}

func TestPrincipal_String(t *testing.T) {
	t.Parallel()
	p, err := urn.NewPrincipal(urn.PrincipalTypeUser, "user_abc")
	require.NoError(t, err)
	require.Equal(t, "user:user_abc", p.String())

	p2, err := urn.NewPrincipal(urn.PrincipalTypeRole, "admin")
	require.NoError(t, err)
	require.Equal(t, "role:admin", p2.String())
}

func TestPrincipal_IsZero(t *testing.T) {
	t.Parallel()
	require.True(t, urn.Principal{}.IsZero())

	p, err := urn.NewPrincipal(urn.PrincipalTypeUser, "abc")
	require.NoError(t, err)
	require.False(t, p.IsZero())
}

func TestPrincipal_Scan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   any
		wantTyp urn.PrincipalType
		wantID  string
		wantErr bool
	}{
		{
			name:    "string input",
			input:   "user:user_abc",
			wantTyp: urn.PrincipalTypeUser,
			wantID:  "user_abc",
			wantErr: false,
		},
		{
			name:    "byte slice input",
			input:   []byte("role:admin"),
			wantTyp: urn.PrincipalTypeRole,
			wantID:  "admin",
			wantErr: false,
		},
		{
			name:    "nil input",
			input:   nil,
			wantErr: false,
		},
		{
			name:    "unsupported type",
			input:   123,
			wantErr: true,
		},
		{
			name:    "invalid string",
			input:   "nocolon",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var p urn.Principal
			err := p.Scan(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.input != nil {
				require.Equal(t, tt.wantTyp, p.Type)
				require.Equal(t, tt.wantID, p.ID)
			}
		})
	}
}

func TestPrincipal_Value(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		typ     urn.PrincipalType
		id      string
		want    driver.Value
		wantErr bool
	}{
		{
			name: "valid user",
			typ:  urn.PrincipalTypeUser,
			id:   "user_abc",
			want: "user:user_abc",
		},
		{
			name: "valid role",
			typ:  urn.PrincipalTypeRole,
			id:   "admin",
			want: "role:admin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := urn.NewPrincipal(tt.typ, tt.id)
			require.NoError(t, err)

			got, err := p.Value()
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestPrincipal_roundTrip(t *testing.T) {
	t.Parallel()
	original, err := urn.NewPrincipal(urn.PrincipalTypeUser, "user_abc123")
	require.NoError(t, err)

	// String round trip
	parsed, err := urn.ParsePrincipal(original.String())
	require.NoError(t, err)
	require.Equal(t, original.Type, parsed.Type)
	require.Equal(t, original.ID, parsed.ID)

	// Database round trip
	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.Principal
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.Type, fromDB.Type)
	require.Equal(t, original.ID, fromDB.ID)
}
