package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestSpendRuleRoundTrip(t *testing.T) {
	t.Parallel()

	original := urn.NewSpendRule("eng-monthly-cap", 3)

	require.Equal(t, "spend_rule:eng-monthly-cap:v3", original.String())

	parsed, err := urn.ParseSpendRule(original.String())
	require.NoError(t, err)
	require.Equal(t, original.Slug, parsed.Slug)
	require.Equal(t, original.Version, parsed.Version)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"spend_rule:eng-monthly-cap:v3"`, string(data))

	var fromJSON urn.SpendRule
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.Slug, fromJSON.Slug)
	require.Equal(t, original.Version, fromJSON.Version)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.SpendRule
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.Slug, fromText.Slug)
	require.Equal(t, original.Version, fromText.Version)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.SpendRule
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.Slug, fromDB.Slug)
	require.Equal(t, original.Version, fromDB.Version)
}

func TestSpendRuleRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "wrong prefix", value: "toolset:eng-monthly-cap:1"},
		{name: "missing version", value: "spend_rule:eng-monthly-cap"},
		{name: "empty slug", value: "spend_rule::v1"},
		{name: "uppercase slug", value: "spend_rule:Eng-Monthly-Cap:v1"},
		{name: "slug with spaces", value: "spend_rule:eng monthly cap:v1"},
		{name: "empty version", value: "spend_rule:eng-monthly-cap:"},
		{name: "non-numeric version", value: "spend_rule:eng-monthly-cap:vX"},
		{name: "missing version prefix", value: "spend_rule:eng-monthly-cap:1"},
		{name: "zero version", value: "spend_rule:eng-monthly-cap:v0"},
		{name: "negative version", value: "spend_rule:eng-monthly-cap:v-1"},
		{name: "trailing segment", value: "spend_rule:eng-monthly-cap:v1:extra"},
	}
	for _, tc := range cases {
		_, err := urn.ParseSpendRule(tc.value)
		require.ErrorIs(t, err, urn.ErrInvalid, "case %s", tc.name)
	}

	_, err := urn.NewSpendRule("", 1).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewSpendRule("eng-monthly-cap", 0).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewSpendRule("Not A Slug", 1).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
