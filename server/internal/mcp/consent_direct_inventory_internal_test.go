package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestBindDirectConsentInventory(t *testing.T) {
	t.Parallel()
	first, second := uuid.New(), uuid.New()
	for _, tc := range []struct {
		name      string
		before    *uuid.UUID
		after     *authClientResolution
		complete  bool
		wantError bool
	}{
		{name: "new typed attempt", after: &authClientResolution{clientID: first}},
		{name: "same typed choice", before: &first, after: &authClientResolution{clientID: first}},
		{name: "different typed choice", before: &first, after: &authClientResolution{clientID: second}, wantError: true},
		{name: "cannot downgrade typed attempt", before: &first, wantError: true},
		{name: "cannot relabel legacy capture", complete: true, after: &authClientResolution{clientID: first}, wantError: true},
		{name: "legacy capture unchanged", complete: true},
		{name: "no auth is explicitly bound", after: &authClientResolution{outcome: authClientNoAuth}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			draft := consentToolInventory{DirectAuthClient: tc.before, Complete: tc.complete}
			err := bindDirectConsentInventory(&draft, tc.after)
			if tc.wantError {
				require.ErrorIs(t, err, errConsentConfigurationChanged)
				return
			}
			require.NoError(t, err)
			if tc.after == nil {
				require.Nil(t, draft.DirectAuthClient)
			} else {
				require.NotNil(t, draft.DirectAuthClient)
				require.Equal(t, tc.after.clientID, *draft.DirectAuthClient)
			}
		})
	}
}
