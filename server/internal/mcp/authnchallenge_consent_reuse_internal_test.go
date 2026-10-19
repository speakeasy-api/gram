package mcp

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestConsentInventoryFingerprintRequiresCompleteEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		inventory *consentToolInventory
	}{
		{name: "missing"},
		{name: "incomplete", inventory: &consentToolInventory{Resource: "mcp_server:example", BackendHash: "backend"}},
		{name: "old snapshot", inventory: &consentToolInventory{Resource: "mcp_server:example", BackendHash: "backend", Complete: true, Tools: []consentInventoryTool{{Name: "read"}}}},
		{name: "missing backend", inventory: &consentToolInventory{Resource: "mcp_server:example", Complete: true}},
	} {
		t.Run(tc.name, func(t *testing.T) { t.Parallel(); require.Empty(t, consentInventoryFingerprint(tc.inventory)) })
	}
}

func TestConsentInventoryFingerprintDetectsChanges(t *testing.T) {
	t.Parallel()
	baseline := consentToolInventory{Resource: "mcp_server:example", BackendHash: "backend", Complete: true, Tools: []consentInventoryTool{
		{Name: "read", DefinitionHash: "schema-a"}, {Name: "write", DefinitionHash: "schema-b"},
	}}
	original := consentInventoryFingerprint(&baseline)
	require.NotEmpty(t, original)
	for _, tc := range []struct {
		name   string
		mutate func(*consentToolInventory)
	}{
		{name: "new tool", mutate: func(i *consentToolInventory) {
			i.Tools = append(i.Tools, consentInventoryTool{Name: "delete", DefinitionHash: "schema-c"})
		}},
		{name: "removed tool", mutate: func(i *consentToolInventory) { i.Tools = i.Tools[:1] }},
		{name: "changed schema", mutate: func(i *consentToolInventory) { i.Tools[0].DefinitionHash = "schema-changed" }},
		{name: "changed backend", mutate: func(i *consentToolInventory) { i.BackendHash = "new-backend" }},
		{name: "sibling", mutate: func(i *consentToolInventory) { i.Resource = "mcp_server:sibling" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			changed := baseline
			changed.Tools = append([]consentInventoryTool(nil), baseline.Tools...)
			tc.mutate(&changed)
			require.NotEqual(t, original, consentInventoryFingerprint(&changed))
		})
	}
	reordered := baseline
	reordered.Tools = []consentInventoryTool{baseline.Tools[1], baseline.Tools[0]}
	require.Equal(t, original, consentInventoryFingerprint(&reordered))
	canonical := baseline
	canonical.Tools = []consentInventoryTool{baseline.Tools[0], baseline.Tools[1]}
	canonical.Tools[0].Annotations = []string{}
	require.Equal(t, original, consentInventoryFingerprint(&canonical))
}

func TestConsentRemoteFingerprintRequiresEveryConnection(t *testing.T) {
	t.Parallel()
	endpoint := &ResolvedMcpEndpoint{UpstreamResource: "https://upstream.example.com/mcp"}
	complete := []remoteSessionCard{{consentPermissionsHash: "read", consentReuseReady: true}, {consentPermissionsHash: "write", consentReuseReady: true}}
	require.NotEmpty(t, consentRemoteFingerprint(endpoint, complete))
	missing := append([]remoteSessionCard(nil), complete...)
	missing[1].consentReuseReady = false
	require.Empty(t, consentRemoteFingerprint(endpoint, missing))
	changed := append([]remoteSessionCard(nil), complete...)
	changed[1].consentPermissionsHash = "admin"
	require.NotEqual(t, consentRemoteFingerprint(endpoint, complete), consentRemoteFingerprint(endpoint, changed))
}

func TestConsentRemoteFingerprintBindsIssuerAndResource(t *testing.T) {
	t.Parallel()
	baseline := ResolvedMcpEndpoint{UserSessionIssuerID: uuid.New(), UpstreamResource: "https://upstream.example.com/mcp"}
	cards := []remoteSessionCard{{consentPermissionsHash: "read", consentReuseReady: true}}
	original := consentRemoteFingerprint(&baseline, cards)
	require.NotEmpty(t, original)
	for _, tc := range []struct {
		name   string
		mutate func(*ResolvedMcpEndpoint)
	}{
		{name: "issuer", mutate: func(e *ResolvedMcpEndpoint) { e.UserSessionIssuerID = uuid.New() }},
		{name: "resource", mutate: func(e *ResolvedMcpEndpoint) { e.UpstreamResource = "https://other.example.com/mcp" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			changed := baseline
			tc.mutate(&changed)
			require.NotEqual(t, original, consentRemoteFingerprint(&changed, cards))
		})
	}
}

func TestConsentBrowserProofCannotBeTransferredBetweenFlows(t *testing.T) {
	t.Parallel()
	now := time.Now()
	state := AuthnChallengeState{FlowID: "first", CreatedAt: now}
	req := httptest.NewRequest("GET", "https://issuer.example.com/authorize", nil)
	response := httptest.NewRecorder()
	require.NoError(t, bindConsentBrowser(response, &state))
	cookies := response.Result().Cookies()
	require.Len(t, cookies, 1)
	require.True(t, cookies[0].Secure)
	require.True(t, cookies[0].HttpOnly)
	require.False(t, consentBrowserMatches(req, state, now))
	req.AddCookie(cookies[0])
	require.True(t, consentBrowserMatches(req, state, now))
	require.False(t, consentBrowserMatches(req, state, now.Add(state.TTL()+time.Second)))
	state.FlowID = "second"
	require.False(t, consentBrowserMatches(req, state, now))
}

func TestRenderConsentReuse(t *testing.T) {
	t.Parallel()
	endpoint := &ResolvedMcpEndpoint{RouteBase: "mcp", Slug: "example"}
	state := AuthnChallengeState{ID: "preview-state", CSRFToken: "preview-csrf"}
	w := httptest.NewRecorder()
	require.NoError(t, renderConsentReuse(w, endpoint, state))
	require.Contains(t, w.Body.String(), "Checking sign-in status...")
	require.Contains(t, w.Body.String(), "Review access")
	require.NotContains(t, w.Body.String(), "Give Access")
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
	require.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
}
