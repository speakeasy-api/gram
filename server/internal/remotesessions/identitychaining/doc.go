// Package identitychaining obtains resource-bound downstream access tokens for
// a human by identity chaining (enterprise-managed authorization, Okta Cross
// App Access): it exchanges the human's retained identity provider ID token
// for an ID-JAG, validates it, and redeems it at the resource authorization
// server under the RFC 7523 JWT bearer grant.
//
// Acquisition is lazy, on a proxied request with no usable interactive token;
// nothing here runs in the background. Downstream credentials live in
// remote_session_ema_credentials and are reused only while their binding,
// delegation and tenant provenance still hold.
package identitychaining
