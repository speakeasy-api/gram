package identitychaining

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauth/protectedresource"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestSelection_PreservesExactResourceIdentity(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"direct", "tunneled", "discovered", "cached"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newChainStoreFixture(t)
			g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
			req := f.req
			resource := f.sel.resource
			if mode == "tunneled" {
				req.RemoteSessionIssuerID = uuid.NullUUID{UUID: f.sel.remoteIssuerID, Valid: true}
			}
			if mode == "discovered" || mode == "cached" {
				req.UpstreamResource = "https://api.resource.example.test/transport"
				g.SetResourceResolver(func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, error) {
					return protectedresource.EndpointResource{Resource: resource, AuthorizationServers: []string{f.sel.issuer}, Discovered: true}, nil
				})
				g.SetCachedResourceResolver(func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, bool, error) {
					return protectedresource.EndpointResource{Resource: resource, AuthorizationServers: []string{f.sel.issuer}, Discovered: true}, true, nil
				})
			}
			resolve := g.ResolveResource
			if mode == "cached" {
				resolve = g.storedResource
			}
			selected, outcome, err := g.selectBindingWith(t.Context(), testenv.NewLogger(t), req, resolve)
			require.NoError(t, err)
			require.Equal(t, success, outcome)
			require.Equal(t, f.sel.resource, selected.resource, "selected bearer resource must retain its trailing slash")

			resource = "https://api.resource.example.test"
			if mode == "direct" || mode == "tunneled" {
				req.UpstreamResource = resource
			}
			_, outcome, err = g.selectBindingWith(t.Context(), testenv.NewLogger(t), req, resolve)
			require.NoError(t, err)
			require.Equal(t, notApplicable, outcome, "a slash-distinct resource must not select the existing binding")

			f.addSlashVariantBinding(t, remotesessions.PreparationStateReady)
			selected, outcome, err = g.selectBindingWith(t.Context(), testenv.NewLogger(t), req, resolve)
			require.NoError(t, err)
			require.Equal(t, success, outcome, "exact resources are independent, not ambiguous")
			require.NotEqual(t, f.sel.bindingID, selected.bindingID)
			require.Equal(t, resource, selected.resource)
		})
	}
}

func TestListEMABindingsForUpstream_ExactResource(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	f.addSlashVariantBinding(t, remotesessions.PreparationStateUnknownGrants)
	q := repo.New(f.db)
	for _, resource := range []string{f.sel.resource, "https://api.resource.example.test"} {
		bindings, err := q.ListEMABindingsForUpstream(t.Context(), repo.ListEMABindingsForUpstreamParams{
			ProjectID: f.req.ProjectID, OrganizationID: f.req.OrganizationID, UserSessionIssuerID: f.req.UserSessionIssuerID,
			RemoteSessionIssuerID: uuid.NullUUID{UUID: f.sel.remoteIssuerID, Valid: true}, UpstreamResource: resource,
		})
		require.NoError(t, err)
		require.Len(t, bindings, 1)
		require.Equal(t, resource, bindings[0].Resource)
	}
}
