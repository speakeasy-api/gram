package k8s

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// StubProvisioner is a no-op CustomDomainProvisioner for local environments
// where Kubernetes is unavailable. It records calls so tests can assert behaviour.
type StubProvisioner struct {
	kind   ProvisionerKind
	logger *slog.Logger
	calls  []StubCall
}

// NewStubProvisioner creates a StubProvisioner for use in tests.
func NewStubProvisioner(kind ProvisionerKind, logger *slog.Logger) *StubProvisioner {
	return &StubProvisioner{kind: kind, logger: logger, calls: nil}
}

// StubCall records a single invocation of a StubProvisioner method.
type StubCall struct {
	Method       string
	Domain       string
	ResourceName string
	SecretName   string
	IPAllowlist  []string
}

func (p *StubProvisioner) Kind() ProvisionerKind { return p.kind }

func (p *StubProvisioner) Setup(ctx context.Context, domain string, ipAllowlist []string) (SetupResult, error) {
	p.calls = append(p.calls, StubCall{Method: "Setup", Domain: domain, ResourceName: "", SecretName: "", IPAllowlist: ipAllowlist})
	p.logger.InfoContext(ctx, "stub provisioner: setup (no-op)", attr.SlogURLDomain(domain))
	name, _ := SanitizeDomainForK8sName(domain)
	return SetupResult{ResourceName: name, SecretName: ""}, nil
}

func (p *StubProvisioner) Get(ctx context.Context, resourceName string) error {
	p.calls = append(p.calls, StubCall{Method: "Get", Domain: "", ResourceName: resourceName, SecretName: "", IPAllowlist: nil})
	p.logger.InfoContext(ctx, "stub provisioner: get (no-op)", attr.SlogResourceName(resourceName))
	return nil
}

func (p *StubProvisioner) Delete(ctx context.Context, resourceName, secretName string) error {
	p.calls = append(p.calls, StubCall{Method: "Delete", Domain: "", ResourceName: resourceName, SecretName: secretName, IPAllowlist: nil})
	p.logger.InfoContext(ctx, "stub provisioner: delete (no-op)", attr.SlogResourceName(resourceName))
	return nil
}

// Calls returns all recorded invocations. Intended for test assertions.
func (p *StubProvisioner) Calls() []StubCall { return p.calls }
