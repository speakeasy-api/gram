package mcp_test

import (
	"context"
	"net"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// missingMemberHost is a member host the resolver says does not exist.
const missingMemberHost = "member.example.invalid"

// callMemberAt calls a gateway member whose address is memberURL and returns the call's text and its row.
func callMemberAt(t *testing.T, memberURL string, guardianOpts ...func(*guardian.Policy)) (string, mcpconnectionattempts.Row) {
	t.Helper()

	ctx, ti := newTestMCPServiceWithMeterProviderAndGuardianOptions(t, testenv.NewMeterProvider(t), guardianOpts...)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-address-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-address", "", []uuid.UUID{issuerID})
	createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member", memberURL, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)), 0)
	subject := createTestUser(t, ctx, ti, "meta-address-user-"+uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject)
	insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, clientID, subject, "token-address", memberURL)

	text, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member--ping"))
	require.True(t, isError)
	rows := rec.Rows()
	require.NotEmpty(t, rows, text)
	return text, rows[len(rows)-1]
}

func TestGatewayMemberAtBlockedAddress(t *testing.T) {
	t.Parallel()

	_, testNet, err := net.ParseCIDR("192.0.2.0/24")
	require.NoError(t, err)
	text, row := callMemberAt(t, "https://192.0.2.10/mcp", guardian.WithBlockedCIDRBlocks(testNet))

	require.Equal(t, mcpconnectionattempts.CodeAddressNotAllowed.String(), row.Reason)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
	require.Empty(t, row.ProviderHost)
	require.NotEmpty(t, row.MemberID)
	require.Equal(t, `server "member" has an address that is not allowed by network policy`+contextvalues.ConnectionReferenceMarker+row.AttemptID.String(), text)
	require.NotContains(t, text, "192.0.2.10")
}

func TestGatewayMemberAtMissingHost(t *testing.T) {
	t.Parallel()

	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(ctx context.Context, network, host string) ([]net.IP, error) {
			if host == missingMemberHost {
				return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
			}
			return net.DefaultResolver.LookupIP(ctx, network, host)
		},
	})
	text, row := callMemberAt(t, "https://"+missingMemberHost+"/mcp", guardian.WithResolver(resolver))

	require.Equal(t, mcpconnectionattempts.CodeAddressNotFound.String(), row.Reason)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
	require.Empty(t, row.ProviderHost)
	require.Equal(t, `server "member" has an address that could not be found`+contextvalues.ConnectionReferenceMarker+row.AttemptID.String(), text)
}
