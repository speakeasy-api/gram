package hostingstate

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
)

func TestDerive(t *testing.T) {
	t.Parallel()

	issuer := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	domain := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	none := Address{Slug: pgtype.Text{String: "", Valid: false}, CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}
	endpoint := Address{Slug: pgtype.Text{String: "endpoint", Valid: true}, CustomDomainID: domain}
	canonical := func(vis string, hasEndpoint bool) *Row {
		return &Row{Canonical: true, Visibility: vis, UserSessionIssuerID: issuer, HasEndpoint: hasEndpoint, EndpointSlug: endpoint.Slug.String, EndpointCustomDomainID: domain}
	}

	for _, tt := range []struct {
		name    string
		row     *Row
		hosting Hosting
		address Address
	}{
		{name: "no server is not hosted", row: nil, hosting: NotHosted(), address: none},
		{name: "kept id is not hosted", row: &Row{Canonical: false}, hosting: NotHosted(), address: none},
		{name: "public wrapper with endpoint", row: canonical(visibility.Public, true), hosting: Hosting{Enabled: true, Public: true, UserSessionIssuerID: issuer}, address: endpoint},
		{name: "private wrapper with endpoint", row: canonical(visibility.Private, true), hosting: Hosting{Enabled: true, Public: false, UserSessionIssuerID: issuer}, address: endpoint},
		{name: "disabled wrapper reads private", row: canonical(visibility.Disabled, true), hosting: Hosting{Enabled: false, Public: false, UserSessionIssuerID: issuer}, address: endpoint},
		{name: "wrapper without endpoint has no address", row: canonical(visibility.Public, false), hosting: Hosting{Enabled: true, Public: true, UserSessionIssuerID: issuer}, address: none},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hosting, address := Derive(uuid.NullUUID{UUID: uuid.Nil, Valid: false}, tt.row)
			require.Equal(t, tt.hosting, hosting)
			require.Equal(t, tt.address, address)
		})
	}
}

func TestFromServerDisabledWithExternalOAuthReadsPublic(t *testing.T) {
	t.Parallel()

	issuer := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	hosting := FromServer(uuid.NullUUID{UUID: uuid.New(), Valid: true}, visibility.Disabled, issuer)
	require.False(t, hosting.Enabled)
	require.True(t, hosting.Public)
	require.Equal(t, issuer, hosting.UserSessionIssuerID)
}
