package externalmcp

import (
	"net/http"
	"net/url"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

type PulseBackend struct {
	registryURL *url.URL
	tenantID    string
	apiKey      conv.Secret
}

func NewPulseBackend(registryURL *url.URL, tenantID string, api conv.Secret) *PulseBackend {
	return &PulseBackend{
		registryURL: registryURL,
		tenantID:    tenantID,
		apiKey:      api,
	}
}

func (p *PulseBackend) Match(req *http.Request) bool {
	return req.URL.Scheme == p.registryURL.Scheme && req.URL.Host == p.registryURL.Host
}

func (p *PulseBackend) Authorize(req *http.Request) error {
	req.Header.Set("X-Tenant-ID", p.tenantID)
	req.Header.Set("X-API-Key", string(p.apiKey.Reveal()))

	return nil
}
