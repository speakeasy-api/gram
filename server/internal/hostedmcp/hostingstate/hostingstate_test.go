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
	columns := Columns{
		Hosting: Hosting{Enabled: true, Public: true, UserSessionIssuerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}},
		Address: Address{Slug: pgtype.Text{String: "alias", Valid: true}, CustomDomainID: uuid.NullUUID{UUID: uuid.New(), Valid: true}},
	}
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
		{name: "no server keeps the columns", row: nil, hosting: columns.Hosting, address: columns.Address},
		{name: "kept id serves nothing", row: &Row{Canonical: false}, hosting: Hosting{Enabled: false, Public: false, UserSessionIssuerID: columns.UserSessionIssuerID}, address: columns.Address},
		{name: "public wrapper with endpoint", row: canonical(visibility.Public, true), hosting: Hosting{Enabled: true, Public: true, UserSessionIssuerID: issuer}, address: endpoint},
		{name: "private wrapper with endpoint", row: canonical(visibility.Private, true), hosting: Hosting{Enabled: true, Public: false, UserSessionIssuerID: issuer}, address: endpoint},
		{name: "disabled wrapper reads private", row: canonical(visibility.Disabled, true), hosting: Hosting{Enabled: false, Public: false, UserSessionIssuerID: issuer}, address: endpoint},
		{name: "wrapper without endpoint keeps the alias", row: canonical(visibility.Public, false), hosting: Hosting{Enabled: true, Public: true, UserSessionIssuerID: issuer}, address: columns.Address},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hosting, address := Derive(columns, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, tt.row)
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
