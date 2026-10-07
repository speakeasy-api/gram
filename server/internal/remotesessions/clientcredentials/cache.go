package clientcredentials

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// credentialEntry is a minted credential in the cache. Its access token is
// encrypted, and the per-write nonce makes every entry distinct, so Forget's
// compare-and-delete never drops a credential another replica minted.
type credentialEntry struct {
	// Key is the entry's cache key.
	Key string

	// AccessTokenEncrypted is the access token, encrypted.
	AccessTokenEncrypted string

	// Scheme is how the access token is presented upstream.
	Scheme Scheme

	// ExpiresAt is when the entry stops being served.
	ExpiresAt time.Time

	// MintedAt is when the token request was sent.
	MintedAt time.Time

	// ttl is how long the cache keeps the entry. Unexported, so it is not
	// stored and does not take part in compare-and-delete.
	ttl time.Duration
}

func (e credentialEntry) CacheKey() string   { return e.Key }
func (e credentialEntry) TTL() time.Duration { return e.ttl }

// failureEntry is a recent grant failure that a retry cannot fix until the
// client's registration changes. It never holds provider response bodies.
type failureEntry struct {
	// Key is the entry's cache key.
	Key string

	// Configuration reports a registration Gram cannot authenticate with.
	Configuration bool

	// StatusCode is the token endpoint's HTTP status for a provider rejection.
	StatusCode int

	// Code is the provider's canonical RFC 6749 error code, when it sent one.
	Code string
}

func (e failureEntry) CacheKey() string   { return e.Key }
func (e failureEntry) TTL() time.Duration { return failureTTL }

// cacheKeys are the cache keys for one client, credential version, scope set
// and resource.
type cacheKeys struct {
	// credential holds the minted credential.
	credential string

	// failure holds a recent failure a retry cannot fix.
	failure string

	// lease single-flights the grant across replicas.
	lease string
}

// newCacheKeys hashes everything the minted credential depends on, so
// arbitrary resources make bounded, delimiter-safe keys. The client's
// credential version covers the stored secret ciphertext (new on every write),
// the signing key set and its active key, and how and where the client
// authenticates, so rotating any of them misses the cache, including a cached
// failure.
func newCacheKeys(client repo.GetClientCredentialsGrantClientRow, resource string) cacheKeys {
	secretExpiresAt := ""
	if client.ClientSecretExpiresAt.Valid {
		secretExpiresAt = client.ClientSecretExpiresAt.Time.UTC().Format(time.RFC3339Nano)
	}

	keySetID := ""
	if client.JsonWebKeySetID.Valid {
		keySetID = client.JsonWebKeySetID.UUID.String()
	}

	resourceIndicatorSupported := ""
	if client.ResourceIndicatorSupported.Valid {
		resourceIndicatorSupported = strconv.FormatBool(client.ResourceIndicatorSupported.Bool)
	}

	tunnelID := ""
	if client.TunneledMcpServerID.Valid {
		tunnelID = client.TunneledMcpServerID.UUID.String()
	}

	digest := sha256.Sum256([]byte(strings.Join([]string{
		client.OrganizationID.String, client.ClientID.String(),
		client.ExternalClientID, client.TokenEndpointAuthMethod.String, client.ClientSecretEncrypted.String, secretExpiresAt,
		keySetID, client.ActiveKeyID, client.TokenEndpointAuthAudienceFormat.String,
		client.IssuerID.String(), client.IssuerUrl, client.TokenEndpoint.String, resourceIndicatorSupported, tunnelID,
		client.ClientAudience.String, strings.Join(slices.Sorted(slices.Values(client.ClientScope)), " "), resource,
	}, "\n")))
	id := hex.EncodeToString(digest[:])

	return cacheKeys{
		credential: "clientCredentials:" + id,
		failure:    "clientCredentialsFailure:" + id,
		lease:      "clientCredentialsLease:" + id,
	}
}
