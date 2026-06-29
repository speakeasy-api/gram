package risk_test

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk"
)

// hkdfInfo mirrors the unexported constant in the risk package. Duplicating it
// here keeps the test black-box while pinning the exact derivation contract: a
// change to the production info string must be reflected here (and would change
// every fingerprint), which is the intended tripwire.
const hkdfInfo = "gram/risk/fingerprint/tenant"

// keyRingJSON builds the JSON payload ParsePepperKeyRing expects. Keys are
// supplied as raw bytes and base64-encoded here so tests can recompute the
// expected HMAC from the same material.
func keyRingJSON(t *testing.T, current string, keys map[string][]byte) []byte {
	t.Helper()
	encKeys := make(map[string]string, len(keys))
	for v, k := range keys {
		encKeys[v] = base64.StdEncoding.EncodeToString(k)
	}
	raw, err := json.Marshal(map[string]any{"current": current, "keys": encKeys})
	require.NoError(t, err)
	return raw
}

func wantHMAC(key, message []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(message)
	return mac.Sum(nil)
}

// wantTenantedHMAC recomputes the expected tenant-scoped MAC: derive a
// per-tenant key from the pepper via HKDF (tenant ID as salt), then HMAC.
func wantTenantedHMAC(t *testing.T, pepper []byte, tenantID string, message []byte) []byte {
	t.Helper()
	key, err := hkdf.Key(sha256.New, pepper, []byte(tenantID), hkdfInfo, 32)
	require.NoError(t, err)
	return wantHMAC(key, message)
}

func TestParsePepperKeyRing_Valid(t *testing.T) {
	t.Parallel()

	keyV1 := []byte("key-material-for-v1")
	keyV2 := []byte("key-material-for-v2")
	payload := keyRingJSON(t, "v2", map[string][]byte{"v1": keyV1, "v2": keyV2})

	fp, err := risk.ParsePepperKeyRing(payload)
	require.NoError(t, err)

	msg := []byte("fingerprint me")

	// HS256 uses the current version (v2) and reports which version it used.
	got, version, err := fp.HS256(msg)
	require.NoError(t, err)
	assert.Equal(t, wantHMAC(keyV2, msg), got)
	assert.Equal(t, "v2", version)

	// HS256WithVersion can reach an older key.
	gotV1, err := fp.HS256WithVersion("v1", msg)
	require.NoError(t, err)
	assert.Equal(t, wantHMAC(keyV1, msg), gotV1)

	// Different keys must produce different MACs.
	assert.NotEqual(t, got, gotV1)
}

func TestParsePepperKeyRing_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload []byte
		wantErr error
	}{
		{
			name:    "invalid json",
			payload: []byte("{not json"),
			wantErr: risk.ErrInvalidFingerprintPepperJSON,
		},
		{
			name:    "non-base64 key",
			payload: []byte(`{"current":"v1","keys":{"v1":"not valid base64!"}}`),
			wantErr: risk.ErrInvalidFingerprintPepperKeyRing,
		},
		{
			name:    "missing current version",
			payload: []byte(`{"keys":{"v1":"AAEC"}}`),
			wantErr: risk.ErrInvalidFingerprintPepperKeyRing,
		},
		{
			name:    "current version not in keys",
			payload: []byte(`{"current":"v9","keys":{"v1":"00112233"}}`),
			wantErr: risk.ErrInvalidFingerprintPepperKeyRing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := risk.ParsePepperKeyRing(tt.payload)
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestFingerprinter_HS256WithVersion_UnknownVersion(t *testing.T) {
	t.Parallel()

	payload := keyRingJSON(t, "v1", map[string][]byte{"v1": []byte("k")})
	fp, err := risk.ParsePepperKeyRing(payload)
	require.NoError(t, err)

	_, err = fp.HS256WithVersion("does-not-exist", []byte("msg"))
	require.Error(t, err)
	assert.ErrorIs(t, err, risk.ErrFingerprintPepperKeyNotFound)
}

func TestFingerprinter_HS256_PanicsWithoutCurrentVersion(t *testing.T) {
	t.Parallel()

	// A zero-value Fingerprinter has no current version configured; HS256
	// asserts the keyring is usable and must panic rather than silently
	// produce an unkeyed hash.
	var fp risk.Fingerprinter
	assert.Panics(t, func() {
		_, _, _ = fp.HS256([]byte("msg"))
	})
}

func TestFingerprinter_TenantedHS256(t *testing.T) {
	t.Parallel()

	keyV1 := []byte("key-material-for-v1")
	keyV2 := []byte("key-material-for-v2")
	payload := keyRingJSON(t, "v2", map[string][]byte{"v1": keyV1, "v2": keyV2})

	fp, err := risk.ParsePepperKeyRing(payload)
	require.NoError(t, err)

	msg := []byte("fingerprint me")

	// TenantedHS256 uses the current version (v2), derives a per-tenant key, and
	// reports the version it used.
	got, version, err := fp.TenantedHS256("tenant-a", msg)
	require.NoError(t, err)
	assert.Equal(t, wantTenantedHMAC(t, keyV2, "tenant-a", msg), got)
	assert.Equal(t, "v2", version)

	// Stable: same tenant + same message + same version is deterministic.
	again, _, err := fp.TenantedHS256("tenant-a", msg)
	require.NoError(t, err)
	assert.Equal(t, got, again)

	// Tenant isolation: the same secret under a different tenant must produce
	// an unrelated fingerprint.
	otherTenant, _, err := fp.TenantedHS256("tenant-b", msg)
	require.NoError(t, err)
	assert.NotEqual(t, got, otherTenant)

	// Tenant scoping must change the MAC versus the un-tenanted HS256.
	plain, _, err := fp.HS256(msg)
	require.NoError(t, err)
	assert.NotEqual(t, plain, got)

	// TenantedHS256WithVersion can reach an older pepper version, and that
	// produces a different fingerprint than the current version.
	gotV1, err := fp.TenantedHS256WithVersion("v1", "tenant-a", msg)
	require.NoError(t, err)
	assert.Equal(t, wantTenantedHMAC(t, keyV1, "tenant-a", msg), gotV1)
	assert.NotEqual(t, got, gotV1)
}

func TestFingerprinter_TenantedHS256WithVersion_UnknownVersion(t *testing.T) {
	t.Parallel()

	payload := keyRingJSON(t, "v1", map[string][]byte{"v1": []byte("k")})
	fp, err := risk.ParsePepperKeyRing(payload)
	require.NoError(t, err)

	_, err = fp.TenantedHS256WithVersion("does-not-exist", "tenant-a", []byte("msg"))
	require.Error(t, err)
	assert.ErrorIs(t, err, risk.ErrFingerprintPepperKeyNotFound)
}

func TestFingerprinter_TenantedHS256_PanicsWithoutCurrentVersion(t *testing.T) {
	t.Parallel()

	// As with HS256, a zero-value Fingerprinter must panic rather than derive a
	// key from an empty pepper.
	var fp risk.Fingerprinter
	assert.Panics(t, func() {
		_, _, _ = fp.TenantedHS256("tenant-a", []byte("msg"))
	})
}
