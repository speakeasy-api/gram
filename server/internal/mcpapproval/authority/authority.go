// Package authority summarises what an MCP server asks a customer to hand over.
//
// This is the approver's most concrete question, in their words: whether a
// connector is "just read-only or taking action on behalf of" them. It is
// answered from what the server and its authorization server publish — OAuth
// scopes, the credentials the install demands, the transport, and which tools
// answer without authentication at all.
//
// Most evidence in this project is a declaration with nothing enforcing it. One
// item here is different: OAuth scopes bound what a token can do, so an
// approver granting a narrow scope gets a guarantee the authorization server
// keeps. That is worth more than an annotation, and the two should not be
// presented as equally solid.
package authority

import (
	"slices"
	"sort"
	"strings"
)

// Mode is how a server expects to be authenticated.
type Mode string

const (
	// ModeUndeclared means nothing was published about authentication. It is
	// not the same as ModeNone: we were told nothing, rather than told there
	// is nothing.
	ModeUndeclared Mode = "undeclared"

	// ModeNone means the server declares it needs no credential.
	ModeNone Mode = "none"

	// ModeAPIKey means the server wants a static secret supplied at install:
	// a long-lived credential the customer pastes in, which the server holds
	// and which no authorization server bounds.
	ModeAPIKey Mode = "api_key"

	// ModeOAuth means the server delegates through an authorization server,
	// so the credential it receives is scoped and revocable.
	ModeOAuth Mode = "oauth"
)

// Credential is one secret or configuration value an install requires.
type Credential struct {
	// Name is the header or variable the server asks to be populated.
	Name string

	// Secret reports whether the publisher marked the value sensitive.
	Secret bool

	// Required reports whether an install cannot proceed without it.
	Required bool

	// Description is the publisher's explanation, if any. Untrusted text.
	Description string
}

// Declaration is what a server and its authorization server publish about
// their authentication requirements.
type Declaration struct {
	// Transport is the MCP transport, such as `http`, `sse`, or `stdio`.
	Transport string

	// RequiresOAuth reports whether the server advertises OAuth.
	RequiresOAuth bool

	// OAuthVersion is `2.1` for RFC 8414 discovery with dynamic registration,
	// `2.0` for legacy static client configuration, or `none`.
	OAuthVersion string

	// RegistrationEndpoint is the authorization server's dynamic client
	// registration endpoint, empty when it publishes none.
	RegistrationEndpoint string

	// Scopes are the OAuth scopes the authorization server advertises.
	Scopes []string

	// Credentials are the headers and variables the install requires.
	Credentials []Credential

	// UnauthenticatedTools names tools the server answers without any
	// credential.
	UnauthenticatedTools []string
}

// Authority is what a server is asking for, grouped the way an approver reads
// it.
type Authority struct {
	// Mode is how the server expects to be authenticated.
	Mode Mode

	// Transport is the declared transport, lower-cased, or empty.
	Transport string

	// Scopes are the advertised OAuth scopes, sorted and de-duplicated.
	//
	// The one item of evidence with teeth: a token is bounded by the scopes
	// consented to, so granting narrowly is enforced by the authorization
	// server rather than trusted to the MCP server's good behaviour.
	Scopes []string

	// DynamicRegistration reports whether the authorization server actually
	// publishes a registration endpoint.
	//
	// Derived from the endpoint's presence, never from a `supported` flag:
	// registries report that optimistically, returning true for authorization
	// servers that expose no registration endpoint at all.
	DynamicRegistration bool

	// DemandedSecrets are the secret credentials an install cannot proceed
	// without — the concrete answer to "what is this asking me to hand over".
	DemandedSecrets []Credential

	// OptionalSecrets are secret credentials the install accepts but does not
	// require.
	OptionalSecrets []Credential

	// UnauthenticatedTools names tools reachable with no credential at all,
	// which is authority granted to anyone who can reach the endpoint rather
	// than to the customer.
	UnauthenticatedTools []string

	// Undeclared reports that nothing was published about authentication.
	// It must surface as unknown, never as "requires nothing".
	Undeclared bool
}

// Summarise groups a server's published authentication requirements.
func Summarise(declaration Declaration) Authority {
	var demanded, optional []Credential
	for _, credential := range declaration.Credentials {
		if !credential.Secret {
			continue
		}
		if credential.Required {
			demanded = append(demanded, credential)
			continue
		}
		optional = append(optional, credential)
	}

	version := strings.ToLower(strings.TrimSpace(declaration.OAuthVersion))
	oauth := declaration.RequiresOAuth || (version != "" && version != "none")

	// A declaration is only empty when the server published nothing at all.
	// A server that says it needs no credential has told us something, and
	// that is a different state from silence.
	undeclared := !oauth &&
		version == "" &&
		declaration.Transport == "" &&
		len(declaration.Credentials) == 0 &&
		len(declaration.Scopes) == 0 &&
		len(declaration.UnauthenticatedTools) == 0

	return Authority{
		Mode:                mode(oauth, demanded, optional, undeclared),
		Transport:           strings.ToLower(strings.TrimSpace(declaration.Transport)),
		Scopes:              normaliseScopes(declaration.Scopes),
		DynamicRegistration: strings.TrimSpace(declaration.RegistrationEndpoint) != "",
		DemandedSecrets:     demanded,
		OptionalSecrets:     optional,
		UnauthenticatedTools: slices.Clone(
			declaration.UnauthenticatedTools,
		),
		Undeclared: undeclared,
	}
}

// mode picks the authentication mode a server is asking for.
//
// OAuth wins over a demanded secret: a server may want both a delegated grant
// and an install-time key, and the delegated grant is the part that carries the
// customer's own authority.
func mode(oauth bool, demanded []Credential, optional []Credential, undeclared bool) Mode {
	switch {
	case undeclared:
		return ModeUndeclared
	case oauth:
		return ModeOAuth
	case len(demanded) > 0 || len(optional) > 0:
		return ModeAPIKey
	default:
		return ModeNone
	}
}

// normaliseScopes sorts and de-duplicates advertised scopes so the same set
// renders identically however a server ordered it, and so a repeated scope
// does not read as two grants.
func normaliseScopes(scopes []string) []string {
	seen := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		trimmed := strings.TrimSpace(scope)
		if trimmed == "" || slices.Contains(seen, trimmed) {
			continue
		}
		seen = append(seen, trimmed)
	}

	sort.Strings(seen)

	return seen
}
