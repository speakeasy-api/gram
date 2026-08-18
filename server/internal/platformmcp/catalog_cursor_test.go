package platformmcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogCursorBindsContinuationToPrincipalAndNormalizedFilters(t *testing.T) {
	t.Parallel()

	codec, err := newCatalogCursorCodec("test-cursor-key")
	require.NoError(t, err)
	principal := Principal{OrganizationID: "organization", Generation: "generation"}
	value, err := codec.Encode(catalogCursor{
		OrganizationID: principal.OrganizationID,
		Generation:     principal.Generation,
		Query:          normalizeCatalogQuery("  Find   MCP  "),
		ProviderKey:    normalizeCatalogProviderKey(" Reviewed "),
		Position:       catalogPageSize,
	})
	require.NoError(t, err)

	position, err := codec.Decode(value, principal, "find mcp", "reviewed")
	require.NoError(t, err)
	require.Equal(t, catalogPageSize, position)

	_, err = codec.Decode(value, Principal{OrganizationID: principal.OrganizationID, Generation: "other-generation"}, "find mcp", "reviewed")
	require.ErrorIs(t, err, ErrCatalogCursorInvalid)
	_, err = codec.Decode(value, principal, "other query", "reviewed")
	require.ErrorIs(t, err, ErrCatalogCursorInvalid)
	_, err = codec.Decode(value, principal, "find mcp", "other")
	require.ErrorIs(t, err, ErrCatalogCursorInvalid)
}

func TestCatalogSearchPageEnforcesPublicBound(t *testing.T) {
	t.Parallel()

	candidates := make([]CatalogCandidate, catalogPageSize+1)
	page, nextPosition, err := catalogSearchPage(candidates, 0)
	require.NoError(t, err)
	require.Len(t, page, catalogPageSize)
	require.Equal(t, catalogPageSize, nextPosition)

	page, nextPosition, err = catalogSearchPage(candidates, nextPosition)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Zero(t, nextPosition)

	_, _, err = catalogSearchPage(candidates, len(candidates)+1)
	require.ErrorIs(t, err, ErrCatalogCursorInvalid)
}

func TestCatalogCursorUsesStableKeyMaterial(t *testing.T) {
	t.Parallel()

	first, err := newCatalogCursorCodec("stable-key")
	require.NoError(t, err)
	second, err := newCatalogCursorCodec("stable-key")
	require.NoError(t, err)
	principal := Principal{OrganizationID: "organization", Generation: "generation"}
	value, err := first.Encode(catalogCursor{OrganizationID: principal.OrganizationID, Generation: principal.Generation})
	require.NoError(t, err)

	_, err = second.Decode(value, principal, "", "")
	require.NoError(t, err)
}

func TestCatalogCursorRejectsTampering(t *testing.T) {
	t.Parallel()

	codec, err := newCatalogCursorCodec("test-cursor-key")
	require.NoError(t, err)
	principal := Principal{OrganizationID: "organization", Generation: "generation"}
	value, err := codec.Encode(catalogCursor{OrganizationID: principal.OrganizationID, Generation: principal.Generation})
	require.NoError(t, err)

	_, err = codec.Decode(value+"x", principal, "", "")
	require.ErrorIs(t, err, ErrCatalogCursorInvalid)
}

// A connection-less caller has no generation to bind a cursor to. Binding to
// its subject instead keeps pagination working and still refuses a cursor
// minted for a different caller.
func TestCatalogCursorBindsAConnectionlessCallerToItsSubject(t *testing.T) {
	t.Parallel()

	codec, err := newCatalogCursorCodec("test-cursor-key")
	require.NoError(t, err)
	assistant := Principal{OrganizationID: "organization", UserID: "user-1", Surface: SurfaceProjectAssistant}
	require.False(t, assistant.HasConnection())
	require.NotEmpty(t, principalCursorBinding(assistant))

	value, err := codec.Encode(catalogCursor{
		OrganizationID: assistant.OrganizationID,
		Generation:     principalCursorBinding(assistant),
		Query:          normalizeCatalogQuery("find mcp"),
		ProviderKey:    "",
		Position:       catalogPageSize,
	})
	require.NoError(t, err, "a connection-less caller must still be able to paginate")

	position, err := codec.Decode(value, assistant, "find mcp", "")
	require.NoError(t, err)
	require.Equal(t, catalogPageSize, position)

	other := assistant
	other.UserID = "user-2"
	_, err = codec.Decode(value, other, "find mcp", "")
	require.ErrorIs(t, err, ErrCatalogCursorInvalid)

	// An OAuth caller's cursors stay bound to its generation, so they cannot be
	// replayed by the assistant acting for the same user.
	connected := Principal{OrganizationID: "organization", UserID: "user-1", ConnectionID: "connection-1", Generation: "generation-1"}
	_, err = codec.Decode(value, connected, "find mcp", "")
	require.ErrorIs(t, err, ErrCatalogCursorInvalid)
}
