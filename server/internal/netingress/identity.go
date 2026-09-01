package netingress

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

var ErrUnsupportedProvider = errors.New("unsupported network ingress provider")

// NetworkIdentityParser normalizes advisory provider identity into the shared
// request-origin shape. It does not authenticate a Gram principal or grant
// authorization.
type NetworkIdentityParser interface {
	ParseIdentity(http.Header) (*requestorigin.NetworkIdentity, error)
}

// IdentityParsers dispatches provider-owned wire formats without leaking raw
// provider headers into shared private-ingress middleware.
type IdentityParsers map[string]NetworkIdentityParser

func (p IdentityParsers) Parse(provider string, headers http.Header) (*requestorigin.NetworkIdentity, error) {
	parser, ok := p[provider]
	if !ok || parser == nil {
		return nil, fmt.Errorf("%w %q", ErrUnsupportedProvider, provider)
	}
	identity, err := parser.ParseIdentity(headers)
	if err != nil {
		return nil, fmt.Errorf("parse %s network identity: %w", provider, err)
	}
	return identity, nil
}
