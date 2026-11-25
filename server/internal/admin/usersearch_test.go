package admin

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseUserSearchBasic(t *testing.T) {
	t.Parallel()
	got, err := ParseUserSearch(`name:Alex org:"Example Studio"`)
	require.NoError(t, err)
	require.Equal(t, []UserSearchTerm{{Field: "name", Value: "Alex"}, {Field: "org", Value: "Example Studio"}}, got)
}

func TestParseUserSearchConformance(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/user_search.json")
	require.NoError(t, err)
	var cases []struct {
		Query string           `json:"query"`
		Terms []UserSearchTerm `json:"terms"`
		Error string           `json:"error"`
	}
	require.NoError(t, json.Unmarshal(data, &cases))
	for _, tc := range cases {
		t.Run(tc.Query, func(t *testing.T) {
			t.Parallel()
			got, err := ParseUserSearch(tc.Query)
			if tc.Error != "" {
				require.EqualError(t, err, tc.Error)
				require.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.Terms, got)
			}
		})
	}
}
