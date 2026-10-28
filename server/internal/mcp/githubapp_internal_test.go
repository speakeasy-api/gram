package mcp

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDecideGitHubApp(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	live := upstreamclientsrepo.GetAuthClientSelectionRow{
		GithubApp: true, AuthClientID: validUUID(id), ClientID: validUUID(id),
		ClientType:    pgtype.Text{String: authClientTypeServiceAccount, Valid: true},
		ClientEnabled: pgtype.Bool{Bool: true, Valid: true}, Associated: true,
	}
	res := decideAuthClient(live)
	require.Equal(t, authClientResolved, res.outcome)
	require.True(t, res.headersOnly())
	require.False(t, res.choice.clientID.Valid, "GitHub does not fabricate an OAuth registration")
	for _, tc := range []struct {
		name   string
		mutate func(*upstreamclientsrepo.GetAuthClientSelectionRow)
	}{
		{"disabled", func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.ClientEnabled.Bool = false }},
		{"detached", func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.Associated = false }},
		{"deleted", func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.ClientDeleted.Bool = true }},
		{"foreign", func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.ClientID.Valid = false }},
		{"oauth registration", func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.RegistrationID = validUUID(uuid.New()) }},
		{"tunnel", func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.Tunneled = true }},
		{"missing auxiliary header", func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.CredentialHeadersUncovered = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := live
			tc.mutate(&row)
			require.Equal(t, authClientGap, decideAuthClient(row).outcome)
		})
	}
}
