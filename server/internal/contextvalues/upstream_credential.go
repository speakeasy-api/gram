package contextvalues

import "context"

type upstreamCredentialKey struct{}

// UpstreamCredential names the auth client that signed a proxied call in to
// its upstream server. Identifiers only: no credential material is carried.
type UpstreamCredential struct {
	// AuthClientID is the typed auth client that served the call, empty when
	// the credential came from a registration no auth client records yet.
	AuthClientID string

	// AuthClientType is the auth client's type: remote_session for a person's
	// own connection, or a shared type such as service_account or
	// static_credential.
	AuthClientType string

	// ConnectionOwnerUserID is the user whose personal connection served the
	// call. Empty for a shared credential.
	ConnectionOwnerUserID string
}

// SetUpstreamCredential carries the credential a gateway chose for one member
// call down to the interceptor that writes the call's log row, which never
// sees the gateway's resolution.
func SetUpstreamCredential(ctx context.Context, value UpstreamCredential) context.Context {
	return context.WithValue(ctx, upstreamCredentialKey{}, value)
}

// GetUpstreamCredential returns the credential recorded for this call. The
// second result is false when no auth client type was recorded.
func GetUpstreamCredential(ctx context.Context) (UpstreamCredential, bool) {
	value, ok := ctx.Value(upstreamCredentialKey{}).(UpstreamCredential)
	return value, ok && value.AuthClientType != ""
}
