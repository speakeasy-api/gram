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
			name: "id at byte limit",
			build: func() urn.SessionSubject {
				return urn.NewUserSubject(strings.Repeat("é", urn.MaxSessionSubjectIDLength/2))
			},
			wantErr: nil,
		},
		{
			name: "ASCII id over byte limit",
			build: func() urn.SessionSubject {
				return urn.NewUserSubject(strings.Repeat("a", urn.MaxSessionSubjectIDLength+1))
			},
			wantErr: urn.ErrInvalid,
		},
		{
			name: "multibyte id over byte limit",
			build: func() urn.SessionSubject {
				return urn.NewUserSubject(strings.Repeat("é", urn.MaxSessionSubjectIDLength/2+1))
			},
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

// The subject shapes these platforms actually mint are colon-heavy, and the
// grammar splits on the first colon only. Asserted against real values rather
// than trusting that reasoning.
func TestWorkloadSubject_RoundTripsRealPlatformSubjects(t *testing.T) {
	t.Parallel()

	issuerID := uuid.MustParse("0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d9e0f")

	for name, externalSubject := range map[string]string{
		"github actions branch":       "repo:acme/payments-api:ref:refs/heads/main",
		"github actions environment":  "repo:acme/payments-api:environment:production",
		"github actions pull request": "repo:acme/payments-api:pull_request",
		"kubernetes service account":  "system:serviceaccount:payments:checkout-worker",
		"spiffe id":                   "spiffe://acme.example/ns/payments/sa/checkout",
		"opaque numeric":              "1029384756",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			subject := urn.NewWorkloadSubject(issuerID, externalSubject)

			parsed, err := urn.ParseSessionSubject(subject.String())
			require.NoError(t, err)
			require.Equal(t, subject, parsed, "a workload subject must survive format and parse unchanged")

			gotIssuer, gotSubject, err := parsed.Workload()
			require.NoError(t, err)
			require.Equal(t, issuerID, gotIssuer)
			require.Equal(t, externalSubject, gotSubject,
				"the external subject must come back byte-identical, colons included")
		})
	}
}

// The kind has to survive every transport a session subject travels on, not
// just String/Parse.
func TestWorkloadSubject_RoundTripsThroughJSONAndTheValuer(t *testing.T) {
	t.Parallel()

	issuerID := uuid.MustParse("0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d9e0f")
	subject := urn.NewWorkloadSubject(issuerID, "repo:acme/payments-api:ref:refs/heads/main")

	encoded, err := json.Marshal(subject)
	require.NoError(t, err)

	var viaJSON urn.SessionSubject
	require.NoError(t, json.Unmarshal(encoded, &viaJSON))
	require.Equal(t, subject, viaJSON)

	value, err := subject.Value()
	require.NoError(t, err)
	require.Equal(t, driver.Value(subject.String()), value)

	var viaScan urn.SessionSubject
	require.NoError(t, viaScan.Scan(subject.String()))
	require.Equal(t, subject, viaScan)
}

// Two workloads sharing a sub across different issuers must produce different
// session subjects. This is the whole reason the kind carries an issuer.
func TestWorkloadSubject_OneSubjectFromTwoIssuersDiffers(t *testing.T) {
	t.Parallel()

	const shared = "repo:acme/payments-api:ref:refs/heads/main"
	first := urn.NewWorkloadSubject(uuid.MustParse("0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d9e0f"), shared)
	second := urn.NewWorkloadSubject(uuid.MustParse("0192f4c8-1a2b-7c3d-8e4f-aaaaaaaaaaaa"), shared)

	require.NotEqual(t, first.String(), second.String(),
		"an identical sub vouched for by another issuer is another workload")
}

// A malformed workload id must be rejected rather than accepted as an opaque
// string, or the kind would carry no guarantee that an issuer is present.
func TestParseSessionSubject_RejectsMalformedWorkloadIDs(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]string{
		"no issuer reference":    "workload:repo-acme-payments-api",
		"issuer is not a uuid":   "workload:not-a-uuid:repo:acme/payments-api",
		"empty external subject": "workload:0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d9e0f:",
		// The nil uuid parses like any other, so it has to be rejected by
		// name: no remote_session_issuers row is ever the nil uuid, and an
		// uninitialised reference must not produce a valid-looking subject.
		"nil uuid issuer": "workload:00000000-0000-0000-0000-000000000000:repo:acme/api",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := urn.ParseSessionSubject(input)
			require.Error(t, err, "%q must not parse as a workload subject", input)
		})
	}
}

// Workload() must refuse a subject of another kind, so a user or api key
// subject cannot be read as a workload by accident.
func TestSessionSubject_WorkloadRefusesOtherKinds(t *testing.T) {
	t.Parallel()

	_, _, err := urn.NewUserSubject("user_01abc").Workload()
	require.Error(t, err)

	_, _, err = urn.NewAPIKeySubject(uuid.New()).Workload()
	require.Error(t, err)
}

// The id segment is capped and over-long ids are rejected outright rather than
// truncated, so the budget left for the external subject is a real limit that
// admission has to enforce before a session is ever minted.
func TestWorkloadSubject_ExternalSubjectBudgetIsEnforced(t *testing.T) {
	t.Parallel()

	issuerID := uuid.New()

	atLimit := urn.NewWorkloadSubject(issuerID, strings.Repeat("x", urn.MaxWorkloadExternalSubjectLength))
	_, err := urn.ParseSessionSubject(atLimit.String())
	require.NoError(t, err, "a subject exactly at the budget must be accepted")

	overLimit := urn.NewWorkloadSubject(issuerID, strings.Repeat("x", urn.MaxWorkloadExternalSubjectLength+1))
	_, err = urn.ParseSessionSubject(overLimit.String())
	require.Error(t, err, "a subject one byte over the budget must be rejected, not truncated")
}
