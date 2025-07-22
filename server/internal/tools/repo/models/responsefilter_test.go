package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterTypeValues(t *testing.T) {
	t.Parallel()
	// Test that all filter type values are included
	require.Contains(t, FilterTypeValues, string(FilterTypeNone))
	require.Contains(t, FilterTypeValues, string(FilterTypeJQ))

	// Test that the values are sorted
	require.Equal(t, []string{"jq", "none"}, FilterTypeValues)
}

func TestFilterTypeConstants(t *testing.T) {
	t.Parallel()
	require.Equal(t, FilterTypeNone, FilterType("none"))
	require.Equal(t, FilterTypeJQ, FilterType("jq"))
}

func TestResponseFilterStruct(t *testing.T) {
	t.Parallel()
	// Test creating a ResponseFilter with all fields
	rf := ResponseFilter{
		Type:         FilterTypeJQ,
		Schema:       []byte(`{"type": "object"}`),
		StatusCodes:  []string{"200", "201"},
		ContentTypes: []string{"application/json", "application/yaml"},
	}

	require.Equal(t, FilterTypeJQ, rf.Type)
	require.JSONEq(t, `{"type": "object"}`, string(rf.Schema))
	require.Equal(t, []string{"200", "201"}, rf.StatusCodes)
	require.Equal(t, []string{"application/json", "application/yaml"}, rf.ContentTypes)
}

func TestResponseFilterZeroValue(t *testing.T) {
	t.Parallel()
	// Test zero value of ResponseFilter
	var rf ResponseFilter

	require.Equal(t, FilterType(""), rf.Type)
	require.Nil(t, rf.Schema)
	require.Nil(t, rf.StatusCodes)
	require.Nil(t, rf.ContentTypes)
}

func TestResponseFilterJSON(t *testing.T) {
	t.Parallel()
	// Test the internal JSON structure
	jsonFilter := responseFilterJSON{
		Type:         "jq",
		Schema:       "eyJ0eXBlIjogIm9iamVjdCJ9", // base64 encoded {"type": "object"}
		StatusCodes:  []string{"200", "201"},
		ContentTypes: []string{"application/json"},
	}

	require.Equal(t, "jq", jsonFilter.Type)
	require.Equal(t, "eyJ0eXBlIjogIm9iamVjdCJ9", jsonFilter.Schema)
	require.Equal(t, []string{"200", "201"}, jsonFilter.StatusCodes)
	require.Equal(t, []string{"application/json"}, jsonFilter.ContentTypes)
}
