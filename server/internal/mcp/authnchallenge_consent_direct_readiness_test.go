package mcp_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// Exercise the public consent handler with two retained grants for the exact
// same registration. Do not use fixture helpers that move the existing row's
// resource: both grants must survive every render.
func TestServeConsent_DirectSharedRegistrationRetainsTargetReadiness(t *testing.T) {
	t.Parallel()
	for _, backend := range []string{"remote", "tunneled"} {
		for _, enforced := range []bool{false, true} {
			name := backend + "/compatibility"
			if enforced {
				name = backend + "/typed"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				ctx, fx := standaloneConsent(t, "direct-retained", consentUpstreamA, func(ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug string) uuid.UUID {
					if backend == "tunneled" {
						return createTunneledServer(t, ctx, ti, projectID, issuerID, slug, consentUpstreamA)
					}
					return createPublicRemoteMcpServer(t, ctx, ti.conn, projectID, issuerID, slug, consentUpstreamA)
				})
				require.NoError(t, testrepo.New(fx.ti.conn).ExpandRemoteSessionResourceIndexesFixture(ctx))
				var registration uuid.UUID
				if enforced {
					_, err := productfeaturesrepo.New(fx.ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: fx.orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
					require.NoError(t, err)
					fx.ti.service.manual = true
					fx.ti.service.SetAuthClientPermitted(true)
					// This typed registration deliberately has no legacy sign-in link.
					_, registration = addDirectScopePersonal(t, ctx, fx, "retained-typed", fx.shared)
				} else {
					registration = createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "retained-legacy", "", []uuid.UUID{fx.shared})
					issuer := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, registration)
					require.NoError(t, authclientfixtures.New(fx.ti.conn).SetServerRemoteSessionIssuerFixture(ctx, authclientfixtures.SetServerRemoteSessionIssuerFixtureParams{ID: fx.endpoint.McpServerID.UUID, RemoteSessionIssuerID: conv.ToNullUUID(issuer)}))
				}
				token, err := fx.ti.enc.Encrypt([]byte("retained-test-token"))
				require.NoError(t, err)
				q := remotesessionsrepo.New(fx.ti.conn)
				var ids []uuid.UUID
				for _, target := range []struct{ resource, identity string }{
					{consentUpstreamA, "Account A"}, {consentUpstreamB, "Account B"},
				} {
					row, err := q.UpsertRemoteSession(ctx, remotesessionsrepo.UpsertRemoteSessionParams{
						SubjectUrn: fx.subject, UserSessionIssuerID: fx.shared, RemoteSessionClientID: registration,
						AccessTokenEncrypted: token, AccessExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
						Resource: conv.ToPGText(target.resource), Scopes: []string{},
						UpstreamDisplayName: conv.ToPGText(target.identity), IdentitySource: conv.ToPGText("id_token"),
					})
					require.NoError(t, err)
					ids = append(ids, row.ID)
				}
				require.NotEqual(t, ids[0], ids[1], "the expanded session key must retain both targets")
				for _, target := range []struct{ resource, own, sibling string }{
					{consentUpstreamA, "Account A", "Account B"}, {consentUpstreamB, "Account B", "Account A"},
				} {
					fx.endpoint.UpstreamResource = target.resource
					code, html, _ := render(t, fx)
					require.Equal(t, http.StatusOK, code)
					require.Contains(t, html, "1 of 1 connected")
					require.Contains(t, html, target.own)
					require.NotContains(t, html, target.sibling)
					require.NotContains(t, html, consentReconnectCopy)
				}
			})
		}
	}
}
