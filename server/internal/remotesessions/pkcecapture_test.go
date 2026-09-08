package remotesessions_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
)

// The code_challenge_methods_supported column is nullable on purpose, and the
// create path is where NULL enters: an omitted payload field means the value
// was never captured, which must stay distinct from an empty array ("the
// issuer advertises no methods" — a refusal state once PKCE enforcement
// lands).
func TestCreateRemoteSessionIssuer_PKCEOmittedStoresNull(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-pkce-omitted"))
	require.NoError(t, err)
	require.Nil(t, created.CodeChallengeMethodsSupported)
}

func TestCreateRemoteSessionIssuer_PKCEEmptyStoresCapturedEmpty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	payload := newIssuerPayload("idp-pkce-empty")
	payload.CodeChallengeMethodsSupported = []string{}
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, created.CodeChallengeMethodsSupported, "an explicit empty array is a captured value, not NULL")
	require.Empty(t, created.CodeChallengeMethodsSupported)
}

func TestCreateRemoteSessionIssuer_PKCEValueStored(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	payload := newIssuerPayload("idp-pkce-value")
	payload.CodeChallengeMethodsSupported = []string{"S256"}
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, []string{"S256"}, created.CodeChallengeMethodsSupported)
}

// The update query uses COALESCE narg semantics like its sibling arrays: an
// omitted field keeps the stored value (including NULL), a present one — empty
// included — overwrites it.
func TestUpdateRemoteSessionIssuer_PKCENargSemantics(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	payload := newIssuerPayload("idp-pkce-narg")
	payload.CodeChallengeMethodsSupported = []string{"S256"}
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)

	name := "Renamed"
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:   created.ID,
		Name: &name,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"S256"}, updated.CodeChallengeMethodsSupported, "an update omitting the field keeps the stored value")

	updated, err = ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:                            created.ID,
		CodeChallengeMethodsSupported: []string{},
	})
	require.NoError(t, err)
	require.NotNil(t, updated.CodeChallengeMethodsSupported)
	require.Empty(t, updated.CodeChallengeMethodsSupported, "a present empty array overwrites to captured-empty")
}

// The update narg can never write NULL, so an issuer whose field was never
// captured stays NULL through unrelated updates.
func TestUpdateRemoteSessionIssuer_PKCEUncapturedSurvivesUpdate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-pkce-uncaptured"))
	require.NoError(t, err)
	require.Nil(t, created.CodeChallengeMethodsSupported)

	name := "Renamed"
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:   created.ID,
		Name: &name,
	})
	require.NoError(t, err)
	require.Nil(t, updated.CodeChallengeMethodsSupported, "an unrelated update must not fabricate a capture")
}

// The advisory warning is the operator-facing half of the capture: RFC 8414
// makes the field OPTIONAL, so absence is spec-legal, but MCP requires clients
// to verify PKCE support, and the warning is where an operator learns their
// issuer would fail that check before enforcement exists.
func TestFetchRemoteSessionIssuerMetadata_WarnsWhenPKCEAbsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, func(doc map[string]any) {
		delete(doc, "code_challenge_methods_supported")
	})

	draft, err := ti.service.FetchRemoteSessionIssuerMetadata(ctx, &gen.FetchRemoteSessionIssuerMetadataPayload{
		Issuer:           upstream.URL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Nil(t, draft.CodeChallengeMethodsSupported, "a draft reports the document as-is; absence stays nil")
	require.True(t, hasWarningContaining(draft.DiscoveryWarnings, "code_challenge_methods_supported missing"), "warnings: %v", draft.DiscoveryWarnings)
}

func TestFetchRemoteSessionIssuerMetadata_WarnsWhenS256NotAdvertised(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, func(doc map[string]any) {
		doc["code_challenge_methods_supported"] = []string{"plain"}
	})

	draft, err := ti.service.FetchRemoteSessionIssuerMetadata(ctx, &gen.FetchRemoteSessionIssuerMetadataPayload{
		Issuer:           upstream.URL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"plain"}, draft.CodeChallengeMethodsSupported)
	require.True(t, hasWarningContaining(draft.DiscoveryWarnings, "does not list S256"), "warnings: %v", draft.DiscoveryWarnings)
}

// The design suppresses omitempty on this field (Meta struct:tag:json) so the
// wire keeps NULL ("never captured", serialized null) distinct from the empty
// array ("captured; the issuer advertises nothing"). With omitempty both
// states would serialize as an absent field and the dashboard's "None
// advertised" state would be unreachable. encoding/json guarantees a
// non-omitempty nil slice marshals as null and an empty one as [], so pinning
// the tag pins the wire behavior — and catches a regeneration that silently
// reverts the design Meta.
func TestRemoteSessionIssuerJSON_PreservesPKCENull(t *testing.T) {
	t.Parallel()

	issuerField, ok := reflect.TypeFor[types.RemoteSessionIssuer]().FieldByName("CodeChallengeMethodsSupported")
	require.True(t, ok)
	require.Equal(t, "code_challenge_methods_supported", issuerField.Tag.Get("json"), "omitempty here would collapse captured-empty into never-captured on the wire")

	draftField, ok := reflect.TypeFor[types.RemoteSessionIssuerDraft]().FieldByName("CodeChallengeMethodsSupported")
	require.True(t, ok)
	require.Equal(t, "code_challenge_methods_supported", draftField.Tag.Get("json"), "omitempty here would collapse advertised-empty into absent on the wire")
}

func hasWarningContaining(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}
