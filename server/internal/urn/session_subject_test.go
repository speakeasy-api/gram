package urn_test

import (
	"database/sql/driver"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestNewSessionSubject(t *testing.T) {
	t.Parallel()

	apikeyID := uuid.New()

	tests := []struct {
		name    string
		build   func() urn.SessionSubject
		wantErr error
	}{
		{
			name:    "valid user",
			build:   func() urn.SessionSubject { return urn.NewUserSubject("user_01abc") },
			wantErr: nil,
		},
		{
			name:    "valid apikey",
			build:   func() urn.SessionSubject { return urn.NewAPIKeySubject(apikeyID) },
			wantErr: nil,
		},
		{
			name:    "valid anonymous",
			build:   func() urn.SessionSubject { return urn.NewAnonymousSubject("mcp-session-uuid") },
			wantErr: nil,
		},
		{
			name:    "user empty id",
			build:   func() urn.SessionSubject { return urn.NewUserSubject("") },
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "anonymous empty id",
			build:   func() urn.SessionSubject { return urn.NewAnonymousSubject("") },
			wantErr: urn.ErrInvalid,
		},
		{
			name:    "id too long",
			build:   func() urn.SessionSubject { return urn.NewUserSubject(strings.Repeat("a", 129)) },
			wantErr: urn.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := tt.build()

			if tt.wantErr != nil {
				_, err := s.MarshalJSON()
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NotEmpty(t, s.String())

			_, err := s.MarshalJSON()
			require.NoError(t, err)
			_, err = s.MarshalText()
			require.NoError(t, err)
			_, err = s.Value()
			require.NoError(t, err)
		})
	}
}

func TestSessionSubject_String(t *testing.T) {
	t.Parallel()

	apikeyID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	tests := []struct {
		name string
		sub  urn.SessionSubject
		want string
	}{
		{
			name: "user",
			sub:  urn.NewUserSubject("user_01abc"),
			want: "user:user_01abc",
		},
		{
			name: "apikey",
			sub:  urn.NewAPIKeySubject(apikeyID),
			want: "apikey:11111111-1111-1111-1111-111111111111",
		},
		{
			name: "anonymous",
			sub:  urn.NewAnonymousSubject("mcp-session-id"),
			want: "anonymous:mcp-session-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tt.sub.String())
		})
	}
}

func TestParseSessionSubject(t *testing.T) {
	t.Parallel()

	apikeyID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	tests := []struct {
		name    string
		input   string
		want    urn.SessionSubject
		wantErr bool
	}{
		{
			name:    "valid user",
			input:   "user:user_01abc",
			want:    urn.NewUserSubject("user_01abc"),
			wantErr: false,
		},
		{
			name:    "valid apikey",
			input:   "apikey:11111111-1111-1111-1111-111111111111",
			want:    urn.NewAPIKeySubject(apikeyID),
			wantErr: false,
		},
		{
			name:    "valid anonymous",
			input:   "anonymous:mcp-session-id",
			want:    urn.NewAnonymousSubject("mcp-session-id"),
			wantErr: false,
		},
		{
			name:    "role rejected",
			input:   "role:admin",
			wantErr: true,
		},
		{
			name:    "apikey non-uuid rejected",
			input:   "apikey:not-a-uuid",
			wantErr: true,
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
			name:    "unknown kind",
			input:   "team:backend",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := urn.ParseSessionSubject(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want.Kind, got.Kind)
			require.Equal(t, tt.want.ID, got.ID)
		})
	}
}

func TestSessionSubject_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sub     urn.SessionSubject
		want    string
		wantErr error
	}{
		{
			name: "user",
			sub:  urn.NewUserSubject("user_01abc"),
			want: `"user:user_01abc"`,
		},
		{
			name: "anonymous",
			sub:  urn.NewAnonymousSubject("session-id"),
			want: `"anonymous:session-id"`,
		},
		{
			name:    "invalid empty id",
			sub:     urn.NewUserSubject(""),
			wantErr: urn.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.sub.MarshalJSON()
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, string(got))
		})
	}
}

func TestSessionSubject_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	apikeyID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	tests := []struct {
		name    string
		input   string
		want    urn.SessionSubject
		wantErr bool
	}{
		{
			name:  "user",
			input: `"user:user_01abc"`,
			want:  urn.NewUserSubject("user_01abc"),
		},
		{
			name:  "apikey",
			input: `"apikey:11111111-1111-1111-1111-111111111111"`,
			want:  urn.NewAPIKeySubject(apikeyID),
		},
		{
			name:    "role rejected",
			input:   `"role:admin"`,
			wantErr: true,
		},
		{
			name:    "non-string",
			input:   `123`,
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
			var got urn.SessionSubject
			err := got.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want.Kind, got.Kind)
			require.Equal(t, tt.want.ID, got.ID)
		})
	}
}

func TestSessionSubject_Scan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   any
		want    urn.SessionSubject
		wantErr bool
	}{
		{
			name:  "string",
			input: "user:user_01abc",
			want:  urn.NewUserSubject("user_01abc"),
		},
		{
			name:  "byte slice",
			input: []byte("anonymous:session-id"),
			want:  urn.NewAnonymousSubject("session-id"),
		},
		{
			name:  "nil",
			input: nil,
			want:  urn.SessionSubject{},
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
			name:    "role rejected",
			input:   "role:admin",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got urn.SessionSubject
			err := got.Scan(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.input != nil {
				require.Equal(t, tt.want.Kind, got.Kind)
				require.Equal(t, tt.want.ID, got.ID)
			}
		})
	}
}

func TestSessionSubject_Value(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sub     urn.SessionSubject
		want    driver.Value
		wantErr bool
	}{
		{
			name: "user",
			sub:  urn.NewUserSubject("user_01abc"),
			want: "user:user_01abc",
		},
		{
			name: "anonymous",
			sub:  urn.NewAnonymousSubject("session-id"),
			want: "anonymous:session-id",
		},
		{
			name:    "invalid empty id",
			sub:     urn.NewUserSubject(""),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.sub.Value()
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSessionSubject_IsZero(t *testing.T) {
	t.Parallel()

	require.True(t, urn.SessionSubject{}.IsZero())
	require.False(t, urn.NewUserSubject("abc").IsZero())
}

func TestSessionSubject_RoundTrip(t *testing.T) {
	t.Parallel()

	apikeyID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	original := urn.NewAPIKeySubject(apikeyID)

	jsonData, err := json.Marshal(original)
	require.NoError(t, err)

	var fromJSON urn.SessionSubject
	err = json.Unmarshal(jsonData, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.String(), fromJSON.String())

	textData, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.SessionSubject
	err = fromText.UnmarshalText(textData)
	require.NoError(t, err)
	require.Equal(t, original.String(), fromText.String())

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.SessionSubject
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.String(), fromDB.String())
}
