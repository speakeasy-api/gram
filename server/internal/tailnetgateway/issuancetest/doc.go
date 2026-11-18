// Package issuancetest runs real gateway slots against an in-process
// Tailscale control server and a Pebble ACME server, and counts the
// certificates Pebble issues. It is opt-in: run it with
//
//	mise run test:server -tags tailnetgateway_issuance ./internal/tailnetgateway/issuancetest/
//
// It lives in its own package because tsnet only shares certificates through
// the state store when it believes it runs in Kubernetes, which it decides
// once per process from the environment.
package issuancetest
