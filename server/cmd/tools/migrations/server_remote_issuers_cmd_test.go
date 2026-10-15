package main

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

func TestDecideServerRemoteIssuer(t *testing.T) {
	t.Parallel()

	stored := uuid.New()
	matched := uuid.New()
	row := func(current, match uuid.NullUUID, servers int64) mcpserversrepo.ListMCPServerRemoteSessionIssuerBackfillRow {
		return mcpserversrepo.ListMCPServerRemoteSessionIssuerBackfillRow{
			ID:                           uuid.New(),
			ProjectID:                    uuid.New(),
			OrganizationID:               "org",
			UserSessionIssuerID:          uuid.NullUUID{UUID: uuid.New(), Valid: true},
			CurrentRemoteSessionIssuerID: current,
			MatchedRemoteSessionIssuerID: match,
			RemoteBacked:                 match.Valid,
			IssuerServerCount:            servers,
		}
	}
	set := func(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: true} }
	unset := uuid.NullUUID{UUID: uuid.Nil, Valid: false}

	cases := []struct {
		name     string
		row      mcpserversrepo.ListMCPServerRemoteSessionIssuerBackfillRow
		provider uuid.NullUUID
		write    bool
	}{
		{name: "the server's own upstream wins over the stored value", row: row(set(stored), set(matched), 3), provider: set(matched), write: true},
		{name: "a match that already landed is not rewritten", row: row(set(matched), set(matched), 3), provider: set(matched), write: false},
		{name: "a match stamps a server that had none", row: row(unset, set(matched), 3), provider: set(matched), write: true},
		{name: "a lone server on its issuer keeps what it has", row: row(set(stored), unset, 1), provider: set(stored), write: false},
		{name: "a lone server with nothing stored stays unset", row: row(unset, unset, 1), provider: unset, write: false},
		{name: "a borrowed value on a shared issuer is cleared", row: row(set(stored), unset, 2), provider: unset, write: true},
		{name: "a shared issuer with nothing stored stays unset", row: row(unset, unset, 4), provider: unset, write: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := decideServerRemoteIssuer(tc.row)
			require.Equal(t, tc.provider, got.provider)
			require.Equal(t, tc.write, got.write)
		})
	}
}
