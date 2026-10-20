// Package clientcredentials obtains the upstream credential a remote session
// client holds for itself, rather than for a session subject, with the OAuth
// 2.0 client credentials grant (RFC 6749 §4.4).
//
// Credentials are minted lazily on request and shared through the cache by
// every replica. A cached credential is keyed by the client, a version of its
// stored credentials, its scopes and the resource the grant sends, which is
// none for an issuer known to reject resource indicators. Rotating the
// client's secret or its signing key therefore misses the cache. While the
// shared cache can hold leases, one replica mints per cache miss and the
// others wait for its result; otherwise each replica mints for itself.
package clientcredentials
