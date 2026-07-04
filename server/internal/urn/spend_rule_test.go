package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestSpendRuleRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	original := urn.NewSpendRule(id, 3)

	require.Equal(t, "spend_rule:33333333-3333-3333-3333-333333333333:v3", original.String())

	parsed, err := urn.ParseSpendRule(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)
	require.Equal(t, original.Version, parsed.Version)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"spend_rule:33333333-3333-3333-3333-333333333333:v3"`, string(data))

	var fromJSON urn.SpendRule
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)
	require.Equal(t, original.Version, fromJSON.Version)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.SpendRule
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)
	require.Equal(t, original.Version, fromText.Version)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.SpendRule
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
	require.Equal(t, original.Version, fromDB.Version)
}

func TestSpendRuleRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "wrong prefix", value: "toolset:33333333-3333-3333-3333-333333333333:v1"},
		{name: "missing version", value: "spend_rule:33333333-3333-3333-3333-333333333333"},
		{name: "bad uuid", value: "spend_rule:not-a-uuid:v1"},
		{name: "version without prefix", value: "spend_rule:33333333-3333-3333-3333-333333333333:1"},
		{name: "empty version", value: "spend_rule:33333333-3333-3333-3333-333333333333:v"},
		{name: "non-numeric version", value: "spend_rule:33333333-3333-3333-3333-333333333333:vX"},
		{name: "zero version", value: "spend_rule:33333333-3333-3333-3333-333333333333:v0"},
		{name: "negative version", value: "spend_rule:33333333-3333-3333-3333-333333333333:v-1"},
		{name: "trailing segment", value: "spend_rule:33333333-3333-3333-3333-333333333333:v1:extra"},
	}
	for _, tc := range cases {
		_, err := urn.ParseSpendRule(tc.value)
		require.ErrorIs(t, err, urn.ErrInvalid, "case %s", tc.name)
	}

	_, err := urn.NewSpendRule(uuid.Nil, 1).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewSpendRule(uuid.MustParse("33333333-3333-3333-3333-333333333333"), 0).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
