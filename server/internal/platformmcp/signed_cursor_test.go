package platformmcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// legacySignedToken is the envelope every cursor codec in this package wrote
// by hand before they shared sealCursor. It is restated here, not imported, so
// a change to the shared helper cannot silently move the wire format that
// tokens already held by callers were minted in.
func legacySignedToken(t *testing.T, domain, keyMaterial string, payload any) string {
	t.Helper()
	key := sha256.Sum256([]byte(domain + ":" + keyMaterial))
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(append(body, mac.Sum(nil)...))
}

func TestSignedCursorKeepsEveryCodecsWireFormat(t *testing.T) {
	t.Parallel()

	const material = "cursor-key-material"
	principal := Principal{OrganizationID: "org-1", ConnectionID: "conn-1", Generation: "gen-1"}
	binding := principalCursorBinding(principal)
	projectID := uuid.New()
	afterID := uuid.New()

	t.Run("catalog", func(t *testing.T) {
		t.Parallel()
		codec, err := newCatalogCursorCodec(material)
		require.NoError(t, err)
		cursor := catalogCursor{OrganizationID: "org-1", Generation: binding, Query: "slack", ProviderKey: "", Position: 40}
		legacy := legacySignedToken(t, "platform-mcp-catalog-cursor", material, cursor)
		minted, err := codec.Encode(cursor)
		require.NoError(t, err)
		require.Equal(t, legacy, minted)
		position, err := codec.Decode(legacy, principal, "slack", "")
		require.NoError(t, err)
		require.Equal(t, 40, position)
	})

	t.Run("plugin", func(t *testing.T) {
		t.Parallel()
		codec, err := newPluginCursorCodec(material)
		require.NoError(t, err)
		cursor := pluginCursor{OrganizationID: "org-1", Binding: binding, ProjectID: projectID.String(), AfterPluginID: afterID.String()}
		legacy := legacySignedToken(t, "platform-mcp-plugin-cursor", material, cursor)
		minted, err := codec.Encode(cursor)
		require.NoError(t, err)
		require.Equal(t, legacy, minted)
		after, err := codec.Decode(legacy, principal, projectID)
		require.NoError(t, err)
		require.Equal(t, afterID, after)
	})

	t.Run("plugin membership", func(t *testing.T) {
		t.Parallel()
		codec, err := newPluginCursorCodec(material)
		require.NoError(t, err)
		service := &PluginsService{cursors: codec}
		pluginID := uuid.New()
		cursor := pluginMembershipCursor{OrganizationID: "org-1", Binding: binding, ProjectID: projectID.String(), PluginID: pluginID.String(), Version: "v1", AfterSortOrder: 3, AfterDisplay: "Slack", AfterID: afterID.String()}
		legacy := legacySignedToken(t, "platform-mcp-plugin-cursor", material, cursor)
		minted, err := service.encodeMembershipCursor(cursor)
		require.NoError(t, err)
		require.Equal(t, legacy, minted)
		decoded, err := service.decodeMembershipCursor(legacy, principal, projectID, pluginID)
		require.NoError(t, err)
		require.Equal(t, cursor, decoded)
	})

	t.Run("risk", func(t *testing.T) {
		t.Parallel()
		codec := newRiskCursorCodec(material)
		cursor := riskCursor{Kind: "findings", OrganizationID: "org-1", Binding: binding, ProjectID: projectID, CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), ID: afterID}
		legacy := legacySignedToken(t, "platform-mcp-risk-read-cursor", material, cursor)
		minted, err := codec.Encode(cursor)
		require.NoError(t, err)
		require.Equal(t, legacy, minted)
		decoded, err := codec.Decode(legacy, principal, "findings", projectID, uuid.Nil)
		require.NoError(t, err)
		require.Equal(t, cursor.ID, decoded.ID)
		require.True(t, cursor.CreatedAt.Equal(decoded.CreatedAt))
	})

	t.Run("inventory", func(t *testing.T) {
		t.Parallel()
		codec, err := newInventoryCursorCodec(material)
		require.NoError(t, err)
		cursor := inventoryCursor{OrganizationID: "org-1", Binding: binding, ProjectID: projectID.String(), Query: "", AfterMCPID: afterID.String()}
		legacy := legacySignedToken(t, "platform-mcp-inventory-cursor", material, cursor)
		minted, err := codec.Encode(cursor)
		require.NoError(t, err)
		require.Equal(t, legacy, minted)
		after, err := codec.Decode(legacy, principal, projectID, "")
		require.NoError(t, err)
		require.Equal(t, afterID, after)
	})

	t.Run("tool inventory", func(t *testing.T) {
		t.Parallel()
		codec, err := newToolInventoryCursorCodec(material)
		require.NoError(t, err)
		cursor := toolInventoryCursor{OrganizationID: "org-1", Binding: binding, ProjectID: projectID.String(), Query: "q", SourceKind: "function", AfterToolURN: "tools:function:a:b"}
		legacy := legacySignedToken(t, "platform-mcp-tool-inventory-cursor", material, cursor)
		minted, err := codec.Encode(cursor)
		require.NoError(t, err)
		require.Equal(t, legacy, minted)
		after, err := codec.Decode(legacy, principal, projectID, "q", "function")
		require.NoError(t, err)
		require.Equal(t, "tools:function:a:b", after)
	})

	t.Run("distribution version", func(t *testing.T) {
		t.Parallel()
		codec, err := newDistributionVersionTokenCodec(material)
		require.NoError(t, err)
		user := Principal{OrganizationID: "org-1", UserID: "user-1"}
		legacy := legacySignedToken(t, "platform-mcp-distribution-version", material, distributionVersionToken{OrganizationID: "org-1", UserID: "user-1", ProjectSlug: "default", Version: 7})
		minted, err := codec.Encode(user, "default", 7)
		require.NoError(t, err)
		require.Equal(t, legacy, minted)
		version, err := codec.Decode(legacy, user, "default")
		require.NoError(t, err)
		require.Equal(t, int64(7), version)
	})
}

func TestOpenCursorRefusesForgedTamperedAndCrossDomainTokens(t *testing.T) {
	t.Parallel()

	type payload struct {
		Position int `json:"position"`
	}
	key := newSignedCursorKey("domain-a", "material")
	token, err := sealCursor(key, payload{Position: 5})
	require.NoError(t, err)

	opened, ok := openCursor[payload](key, token)
	require.True(t, ok)
	require.Equal(t, 5, opened.Position)

	_, ok = openCursor[payload](newSignedCursorKey("domain-b", "material"), token)
	require.False(t, ok, "a token minted for one domain must not open in another")

	raw, err := base64.RawURLEncoding.DecodeString(token)
	require.NoError(t, err)
	raw[0] ^= 0x01
	_, ok = openCursor[payload](key, base64.RawURLEncoding.EncodeToString(raw))
	require.False(t, ok, "a tampered payload must not verify")

	_, ok = openCursor[payload](key, "not-base64!")
	require.False(t, ok)
	_, ok = openCursor[payload](key, "")
	require.False(t, ok)

	require.Nil(t, newSignedCursorKey("domain-a", ""))
	_, err = sealCursor[payload](nil, payload{})
	require.Error(t, err)
	_, ok = openCursor[payload](nil, token)
	require.False(t, ok)
}
