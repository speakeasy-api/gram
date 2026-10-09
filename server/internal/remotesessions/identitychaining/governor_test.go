package identitychaining

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestGovernor_ServesOnlyAnUnambiguousBinding(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc)

	issuer, ok := g.Serves(t.Context(), f.req)
	require.True(t, ok)
	require.Equal(t, f.sel.remoteIssuerID, issuer)

	unbound := f.req
	unbound.UpstreamResource = "https://unbound.example.test"
	_, ok = g.Serves(t.Context(), unbound)
	require.False(t, ok)

	f.addSlashVariantBinding(t, remotesessions.PreparationStateReady)
	_, ok = g.Serves(t.Context(), f.req)
	require.False(t, ok, "ambiguous bindings require configuration and are never presented as served")
	require.True(t, g.Governs(t.Context(), f.req), "the runtime still claims an ambiguous upstream")
}

func TestGovernor_Configured(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc)

	require.True(t, g.Configured(t.Context(), f.req.OrganizationID, f.req.ProjectID, f.req.UserSessionIssuerID))
	require.False(t, g.Configured(t.Context(), f.req.OrganizationID, f.req.ProjectID, uuid.New()))
	require.False(t, g.Configured(t.Context(), f.req.OrganizationID, uuid.New(), f.req.UserSessionIssuerID))
}

func TestGovernor_HasUsableCredential(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc)
	require.False(t, g.HasUsableCredential(t.Context(), f.req), "no credential stored yet")

	require.NoError(t, f.chainer.publish(t.Context(), f.req, f.sel, f.credential("downstream-token")))
	require.True(t, g.HasUsableCredential(t.Context(), f.req))

	rotated, err := encryption.NewWithBytes(bytes.Repeat([]byte{0x24}, 32))
	require.NoError(t, err)
	require.False(t, NewGovernor(testenv.NewLogger(t), f.db, rotated).HasUsableCredential(t.Context(), f.req), "a credential the runtime cannot decrypt is unusable")

	other := f.req
	other.UserID = "user-other"
	require.False(t, g.HasUsableCredential(t.Context(), other))

	f.deleteDelegation(t)
	require.False(t, g.HasUsableCredential(t.Context(), f.req))
	require.Len(t, f.credentials(t), 1)
	require.False(t, f.credentials(t)[0].Deleted, "a status read never retires a credential")
}

func TestNewRequest(t *testing.T) {
	t.Parallel()
	project, usi, tunnel := uuid.New(), uuid.New(), uuid.New()

	direct, ok := NewRequest("org", project, usi, "user", "https://up.example.test/mcp", false, uuid.NullUUID{UUID: tunnel, Valid: true})
	require.True(t, ok)
	require.False(t, direct.RemoteSessionIssuerID.Valid, "a direct upstream selects by resource alone")

	tunneled, ok := NewRequest("org", project, usi, "user", "urn:tunnel", true, uuid.NullUUID{UUID: tunnel, Valid: true})
	require.True(t, ok)
	require.Equal(t, uuid.NullUUID{UUID: tunnel, Valid: true}, tunneled.RemoteSessionIssuerID)

	_, ok = NewRequest("org", project, usi, "user", "urn:tunnel", true, uuid.NullUUID{})
	require.False(t, ok, "a tunnel without a derived issuer never chains")
	_, ok = NewRequest("org", project, usi, "", "https://up.example.test/mcp", false, uuid.NullUUID{})
	require.False(t, ok)
	_, ok = NewRequest("org", project, usi, "user", "/", false, uuid.NullUUID{})
	require.False(t, ok)
}

func TestGovernor_EmptyCredentialIsUnusable(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc)
	require.NoError(t, f.chainer.publish(t.Context(), f.req, f.sel, f.credential("")))
	require.False(t, g.HasUsableCredential(t.Context(), f.req), "an empty decrypted token is unusable and must not panic")
}

func TestInspectIdentityChainingForTenant(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)

	results, err := remotesessions.InspectIdentityChainingForTenant(t.Context(), f.db, f.req.ProjectID, f.req.OrganizationID, f.req.UserSessionIssuerID, f.sel.remoteIssuerID, f.req.UpstreamResource+"/")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, remotesessions.PreparationStateReady, results[0].State)
	require.Equal(t, f.sel.bindingID, results[0].BindingID)
	require.Equal(t, []string{"read"}, results[0].Scopes)

	f.addSlashVariantBinding(t, remotesessions.PreparationStateUnknownGrants)
	results, err = remotesessions.InspectIdentityChainingForTenant(t.Context(), f.db, f.req.ProjectID, f.req.OrganizationID, f.req.UserSessionIssuerID, f.sel.remoteIssuerID, f.req.UpstreamResource)
	require.NoError(t, err)
	require.Len(t, results, 2, "inspection reports bindings in every live state")

	for _, suffix := range []string{"//", "///", "////", "/////"} {
		bindResource(t, f.db, f.req, f.sel.remoteIssuerID, f.sel.clientID, "https://api.resource.example.test"+suffix, remotesessions.PreparationStateUnknownGrants)
	}
	results, err = remotesessions.InspectIdentityChainingForTenant(t.Context(), f.db, f.req.ProjectID, f.req.OrganizationID, f.req.UserSessionIssuerID, f.sel.remoteIssuerID, f.req.UpstreamResource)
	require.NoError(t, err)
	require.Len(t, results, 6, "inspection never truncates the binding list")

	for _, foreign := range []struct {
		project uuid.UUID
		org     string
	}{{uuid.New(), f.req.OrganizationID}, {f.req.ProjectID, "org-" + uuid.NewString()}} {
		results, err = remotesessions.InspectIdentityChainingForTenant(t.Context(), f.db, foreign.project, foreign.org, f.req.UserSessionIssuerID, f.sel.remoteIssuerID, f.req.UpstreamResource)
		require.NoError(t, err)
		require.Empty(t, results, "another tenant never reads these bindings")
	}
	results, err = remotesessions.InspectIdentityChainingForTenant(t.Context(), f.db, f.req.ProjectID, f.req.OrganizationID, f.req.UserSessionIssuerID, uuid.New(), f.req.UpstreamResource)
	require.NoError(t, err)
	require.Empty(t, results)
}
