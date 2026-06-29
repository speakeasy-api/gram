package risk

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/inv"
)

// hkdfInfo is the application-specific context string mixed into HKDF when
// deriving per-tenant fingerprint keys. It domain-separates these keys from any
// other use of the same pepper.
const hkdfInfo = "gram/risk/fingerprint/tenant"

var (
	ErrInvalidFingerprintPepperJSON    = errors.New("invalid fingerprint pepper keyring json")
	ErrInvalidFingerprintPepperKeyRing = errors.New("invalid fingerprint pepper keyring")
	ErrFingerprintPepperKeyNotFound    = errors.New("fingerprint pepper key not found")
)

type Fingerprinter struct {
	currentVersion string
	keys           map[string][]byte
}

// tenantedOptions holds the configurable behaviour for the tenanted
// fingerprinting methods.
type tenantedOptions struct {
	keyCache map[string][]byte
}

// TenantedOption customizes the behaviour of TenantedHS256 and
// TenantedHS256WithVersion.
type TenantedOption func(*tenantedOptions)

// WithKeyCache supplies a cache for per-tenant derived keys so that repeated
// fingerprinting under the same (version, tenant) pair does not re-run HKDF.
// The cache is keyed by version and tenant ID and is read from and written to
// by the tenanted fingerprinting methods. The caller owns the map and is
// responsible for its lifetime and any concurrency control; a fresh map scoped
// to a single batch is the typical usage.
func WithKeyCache(cache map[string][]byte) TenantedOption {
	return func(o *tenantedOptions) {
		o.keyCache = cache
	}
}

func (p Fingerprinter) get(version string) ([]byte, error) {
	key, ok := p.keys[version]
	if !ok {
		return nil, fmt.Errorf("%s: %w", version, ErrFingerprintPepperKeyNotFound)
	}

	return key, nil
}

func (p Fingerprinter) HS256(message []byte) ([]byte, string, error) {
	inv.Require(
		"fingerprint pepper keyring",
		"current version is set", p.currentVersion != "",
		"current version exists in keys", func() bool {
			_, ok := p.keys[p.currentVersion]
			return ok
		},
	)

	s, err := p.HS256WithVersion(p.currentVersion, message)
	if err != nil {
		return nil, "", err
	}

	return s, p.currentVersion, nil
}

func (p Fingerprinter) HS256WithVersion(version string, message []byte) ([]byte, error) {
	key, err := p.get(version)
	if err != nil {
		return nil, err
	}

	mac := hmac.New(sha256.New, key)
	mac.Write(message)
	result := mac.Sum(nil)

	return result, nil
}

// TenantedHS256 fingerprints message under a per-tenant key derived from the
// current pepper version, so the same secret in two different tenants produces
// unrelated fingerprints (tenant isolation).
func (p Fingerprinter) TenantedHS256(tenantID string, message []byte, opts ...TenantedOption) ([]byte, string, error) {
	inv.Require(
		"fingerprint pepper keyring",
		"current version is set", p.currentVersion != "",
		"current version exists in keys", func() bool {
			_, ok := p.keys[p.currentVersion]
			return ok
		},
	)

	s, err := p.TenantedHS256WithVersion(p.currentVersion, tenantID, message, opts...)
	if err != nil {
		return nil, "", err
	}

	return s, p.currentVersion, nil
}

// TenantedHS256WithVersion is like HS256WithVersion but keys the HMAC with a
// per-tenant key instead of the raw pepper. See deriveKey for the derivation.
func (p Fingerprinter) TenantedHS256WithVersion(version string, tenantID string, message []byte, opts ...TenantedOption) ([]byte, error) {
	var o tenantedOptions
	for _, opt := range opts {
		opt(&o)
	}

	key, err := p.deriveKey(version, tenantID, o.keyCache)
	if err != nil {
		return nil, err
	}

	mac := hmac.New(sha256.New, key)
	mac.Write(message)
	result := mac.Sum(nil)

	return result, nil
}

// deriveKey derives a per-tenant 32-byte key from the pepper via HKDF, with the
// tenant ID as salt. Same pepper + same tenant always yields the same key, so
// fingerprints are stable; different tenants get independent keys, so the same
// secret in two tenants produces unrelated fingerprints (tenant isolation).
func (p Fingerprinter) deriveKey(version string, tenantID string, cache map[string][]byte) ([]byte, error) {
	cacheKey := version + "\x00" + tenantID
	if cache != nil {
		if key, ok := cache[cacheKey]; ok {
			return key, nil
		}
	}

	pepper, err := p.get(version)
	if err != nil {
		return nil, err
	}

	key, err := hkdf.Key(sha256.New, pepper, []byte(tenantID), hkdfInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("hkdf: %w", err)
	}

	if cache != nil {
		cache[cacheKey] = key
	}

	return key, nil
}

// ParsePepperKeyRing parses a JSON payload containing the pepper keyring for
// fingerprinting risk findings. The expected format is:
//
//	{
//	  "current": "v2",
//	  "keys": {
//	    "v1": "base64-encoded-key-for-v1",
//	    "v2": "base64-encoded-key-for-v2"
//	  }
//	}
func ParsePepperKeyRing(jsonSecret []byte) (Fingerprinter, error) {
	var empty Fingerprinter

	type rawPepperKeyRing struct {
		CurrentVersion string            `json:"current"`
		Keys           map[string]string `json:"keys"`
	}

	var raw rawPepperKeyRing
	if err := json.Unmarshal(jsonSecret, &raw); err != nil {
		return empty, fmt.Errorf("%w: %w", ErrInvalidFingerprintPepperJSON, err)
	}

	keyring := Fingerprinter{
		currentVersion: raw.CurrentVersion,
		keys:           make(map[string][]byte, len(raw.Keys)),
	}

	for version, keyb64 := range raw.Keys {
		keyBytes, err := base64.StdEncoding.DecodeString(keyb64)
		if err != nil {
			return empty, fmt.Errorf("%w: failed to decode key for version %s: %w", ErrInvalidFingerprintPepperKeyRing, version, err)
		}
		keyring.keys[version] = keyBytes
	}

	if keyring.currentVersion == "" {
		return empty, fmt.Errorf("current version not set: %w", ErrInvalidFingerprintPepperKeyRing)
	}

	if _, ok := keyring.keys[keyring.currentVersion]; !ok {
		return empty, fmt.Errorf("current version %s not found in keys: %w", keyring.currentVersion, ErrInvalidFingerprintPepperKeyRing)
	}

	if len(keyring.keys) == 0 {
		return empty, fmt.Errorf("no keys found in keyring: %w", ErrInvalidFingerprintPepperKeyRing)
	}

	return keyring, nil
}
