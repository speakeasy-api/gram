package k8s

import (
	"context"
	"log/slog"
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
}

func (p *StubProvisioner) Kind() ProvisionerKind { return p.kind }

func (p *StubProvisioner) Setup(ctx context.Context, domain string) (SetupResult, error) {
	p.calls = append(p.calls, StubCall{Method: "Setup", Domain: domain})
	p.logger.InfoContext(ctx, "stub provisioner: setup (no-op)", slog.String("domain", domain))
	name, _ := SanitizeDomainForK8sName(domain)
	return SetupResult{ResourceName: name, SecretName: ""}, nil
}

func (p *StubProvisioner) Get(ctx context.Context, resourceName string) error {
	p.calls = append(p.calls, StubCall{Method: "Get", ResourceName: resourceName})
	p.logger.InfoContext(ctx, "stub provisioner: get (no-op)", slog.String("resource", resourceName))
	return nil
}

func (p *StubProvisioner) Delete(ctx context.Context, resourceName, secretName string) error {
	p.calls = append(p.calls, StubCall{Method: "Delete", ResourceName: resourceName, SecretName: secretName})
	p.logger.InfoContext(ctx, "stub provisioner: delete (no-op)", slog.String("resource", resourceName))
	return nil
}

// Calls returns all recorded invocations. Intended for test assertions.
func (p *StubProvisioner) Calls() []StubCall { return p.calls }
