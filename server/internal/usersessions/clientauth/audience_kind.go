package clientauth

// AudienceKind labels which of the accepted audience values a client actually
// presented. The choice is unobservable to the client, so recording it is the
// only way to learn what real implementations send.
type AudienceKind string

const (
	// AudienceKindIssuer is the endpoint's issuer identifier, the value this
	// server documents as canonical.
	AudienceKindIssuer AudienceKind = "issuer"

	// AudienceKindEndpoint is the URL of the endpoint the request was made to.
	AudienceKindEndpoint AudienceKind = "endpoint"
)
