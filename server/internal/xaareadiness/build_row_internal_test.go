package xaareadiness

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/xaareadiness/repo"
)

func TestBuildRowIssuerID(t *testing.T) {
	t.Parallel()

	r := row{
		server:   repo.ListEligibleServersRow{IssuerID: uuid.New()},
		resource: "https://mcp.example.com",
	}
	unconfirmed := buildRow(&snapshot{}, r)
	require.Equal(t, "https://mcp.example.com", unconfirmed.ResourceIndicator)
	require.NotNil(t, unconfirmed.IssuerID)
	require.Equal(t, r.server.IssuerID.String(), *unconfirmed.IssuerID)
	require.Nil(t, unconfirmed.Audience)

	r.connection = &record{OktaResourceConnection: repo.OktaResourceConnection{Audience: "https://app.example.com"}}
	confirmed := buildRow(&snapshot{}, r)
	require.Equal(t, unconfirmed.IssuerID, confirmed.IssuerID)
	require.Equal(t, r.connection.Audience, *confirmed.Audience)

	r.server.IssuerID = uuid.New()
	other := buildRow(&snapshot{}, r)
	require.Equal(t, unconfirmed.ResourceIndicator, other.ResourceIndicator)
	require.NotEqual(t, *unconfirmed.IssuerID, *other.IssuerID)
}
