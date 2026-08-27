package urn_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestIdentityRoundTrip(t *testing.T) {
	t.Parallel()

	original := urn.NewUserIdentity("user_01abc")

	require.Equal(t, "user:user_01abc", original.String())

	parsed, err := urn.ParseIdentity(original.String())
	require.NoError(t, err)
	require.Equal(t, original.Kind, parsed.Kind)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.JSONEq(t, `"user:user_01abc"`, string(data))

	var fromJSON urn.Identity
	require.NoError(t, json.Unmarshal(data, &fromJSON))
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.Identity
	require.NoError(t, fromText.UnmarshalText(text))
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.Identity
	require.NoError(t, fromDB.Scan(value))
	require.Equal(t, original.ID, fromDB.ID)
}

func TestIdentityKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		kind  urn.IdentityKind
		id    string
	}{
		{name: "user", value: "user:user_01abc", kind: urn.IdentityKindUser, id: "user_01abc"},
		{name: "email", value: "email:dev@acme.corp", kind: urn.IdentityKindEmail, id: "dev@acme.corp"},
		{name: "external", value: "external:svc-7", kind: urn.IdentityKindExternal, id: "svc-7"},
		{name: "apikey", value: "apikey:33333333-3333-3333-3333-333333333333", kind: urn.IdentityKindAPIKey, id: "33333333-3333-3333-3333-333333333333"},
		{name: "agent", value: "agent:agt_01abc", kind: urn.IdentityKindAgent, id: "agt_01abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := urn.ParseIdentity(tt.value)
			require.NoError(t, err)
			require.Equal(t, tt.kind, parsed.Kind)
			require.Equal(t, tt.id, parsed.ID)
			require.Equal(t, tt.value, parsed.String())
		})
	}
}

// Links to the same person are built from whatever casing the source surface
// recorded, so parsing has to converge them on one URN.
func TestIdentityNormalizesEmail(t *testing.T) {
	t.Parallel()

	parsed, err := urn.ParseIdentity("email:  Dev@Acme.Corp ")
	require.NoError(t, err)
	require.Equal(t, "email:dev@acme.corp", parsed.String())
	require.Equal(t, urn.NewEmailIdentity("DEV@ACME.CORP").String(), parsed.String())
}

// External ids are opaque identifiers chosen by the calling agent, so casing
// is significant and must survive parsing.
func TestIdentityKeepsExternalIDVerbatim(t *testing.T) {
	t.Parallel()

	parsed, err := urn.ParseIdentity("external:Svc-7:Prod")
	require.NoError(t, err)
	require.Equal(t, urn.IdentityKindExternal, parsed.Kind)
	require.Equal(t, "Svc-7:Prod", parsed.ID)
}

func TestIdentityRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "no delimiter", value: "user_01abc"},
		{name: "empty id", value: "user:"},
		{name: "unknown kind", value: "role:admin"},
		{name: "email without address", value: "email:dev"},
		{name: "email that is only an at sign", value: "email:@"},
		{name: "email with two at signs", value: "email:a@b@c"},
		{name: "email with a display name", value: "email:Dev <dev@acme.corp>"},
		{name: "apikey that is not a uuid", value: "apikey:not-a-uuid"},
		{name: "id too long", value: "user:" + strings.Repeat("a", 129)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := urn.ParseIdentity(tt.value)
			require.ErrorIs(t, err, urn.ErrInvalid)
		})
	}
}

// Roles are grantable principals, not subjects, and an identity URN must not
// widen the RBAC vocabulary by accepting one.
func TestIdentityRejectsRolePrincipal(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseIdentity(urn.NewPrincipal(urn.PrincipalTypeRole, "organization:admin").String())
	require.ErrorIs(t, err, urn.ErrInvalid)
}

func TestIdentityZeroValue(t *testing.T) {
	t.Parallel()

	var zero urn.Identity
	require.True(t, zero.IsZero())

	_, err := zero.Value()
	require.ErrorIs(t, err, urn.ErrInvalid)

	require.False(t, urn.NewUserIdentity("user_01abc").IsZero())
}

// Parsing normalizes an email id, but a URN built from the exported fields
// skips that. Two links to the same person must not key on different strings.
func TestIdentityRejectsUnnormalizedEmailID(t *testing.T) {
	t.Parallel()

	unnormalized := urn.Identity{Kind: urn.IdentityKindEmail, ID: "Dev@Acme.Corp"}

	_, err := unnormalized.Value()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
