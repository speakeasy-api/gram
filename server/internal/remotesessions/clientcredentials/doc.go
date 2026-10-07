// Package clientcredentials obtains the upstream credential a remote session
// client holds for itself, rather than for a session subject, with the OAuth
// 2.0 client credentials grant (RFC 6749 §4.4).
//
// Credentials are minted lazily on request and shared through the cache by
// every replica. A cached credential is keyed by the client, a version of its
// stored credentials, its scopes and the requested resource, so rotating the
// client's secret or its signing key misses the cache. One replica mints per
// cache miss while the others wait for its result.
package clientcredentials
