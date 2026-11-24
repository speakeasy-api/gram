package platformmcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// swappingAuthorizer publishes a new catalog snapshot while a tool call is being authorized.
type swappingAuthorizer struct {
	*testAuthorizer
	swap func()
}

func (a swappingAuthorizer) AuthorizeExternalCall(ctx context.Context, principal Principal, policy ExternalAuthorization) error {
	a.swap()
	return a.testAuthorizer.AuthorizeExternalCall(ctx, principal, policy)
}

func TestRuntimeToolCallReadsOneAICatalogSnapshot(t *testing.T) {
	t.Parallel()

	initial := aivendors.NewSnapshot(aivendors.Products())
	store := aivendors.NewStore(initial)
	products := aivendors.Products()
	products[0].DisplayName = "Swapped"
	swapped := aivendors.NewSnapshot(products)
	authorizer := swappingAuthorizer{
		testAuthorizer: &testAuthorizer{grants: []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)}},
		swap:           func() { store.Publish(swapped) },
	}
	library := &stubLibraryReader{list: &aitargets.OrganizationList{Entries: nil, Snapshot: aitargets.NewSnapshot(1, nil)}}
	logger := testenv.NewLogger(t)
	reader := NewPostgresReader(logger, nil).WithShadowAI(NewShadowAIService(&stubDetectionReader{}, library, stubAuthorizer{}, allowingBudget()))
	runtime := NewRuntime(logger, &testAuthenticator{principal: testPrincipal()}, testGate{enabled: true}, authorizer, "", "test-cursor-key", reader, nil, nil, nil, nil).WithAICatalog(store)

	response := allowlistRPCRaw(t, runtime.Handler(), runtime, testOtherClientID, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_ai_scan_library","arguments":{}}}`)
	require.Nil(t, response.Error)
	require.Same(t, swapped, store.Snapshot(), "the store changed during the call")
	require.Same(t, initial, library.lastCatalog, "the tool kept the snapshot pinned when the call arrived")
}
