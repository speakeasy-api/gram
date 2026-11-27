package networkingress

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/tailscaleapi"
)

var runtimeQueuePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// maxGatewaySlots bounds node identities per ingress. Each slot is a device
// on the customer's tailnet, so the cap keeps their device list tidy.
const maxGatewaySlots = 16

// DefaultGatewayTag is the ACL tag gateway nodes and services carry unless an
// environment configures its own. Customers own it in their tailnet policy and
// scope the OAuth client to it, and the dashboard shows it in the policy it
// asks them to add.
const DefaultGatewayTag = "tag:speakeasy"

// DefaultHostname is the Tailscale Service label used when a create request
// omits one; new ingresses are reachable at speakeasy.<tailnet>.ts.net.
const DefaultHostname = "speakeasy"

// RuntimeConfig separates optional lifecycle configuration from permission to
// apply provider changes. Its zero value disables lifecycle delivery and mutation.
type RuntimeConfig struct {
	// ReconcileTaskQueue is explicit and never falls back to a general worker queue.
	ReconcileTaskQueue string

	// ProviderMutationsEnabled permits apply and rotation, not admission or
	// cleanup. It is set only where tailnet gateway pods run, since nothing
	// else hosts what apply defines.
	ProviderMutationsEnabled bool

	// Gateway configures the tsnet tailnet gateway provider.
	Gateway GatewayRuntimeConfig
}

// GatewayRuntimeConfig configures the tailnet gateway provider.
type GatewayRuntimeConfig struct {
	// Tag is the ACL tag minted auth keys carry; it must match the gateway pods.
	Tag string

	// Slots is how many node identities (gateway pod ordinals) host each ingress.
	Slots int32

	// APIURL overrides the Tailscale API base URL; empty means the public API.
	APIURL string

	// Namer names hostnames, services and devices, and decides which this
	// stack may touch: a LocalNamer in local development, the
	// ProductionNamer otherwise.
	Namer Namer
}

// Ready reports whether the gateway provider can mint keys and define services.
func (c GatewayRuntimeConfig) Ready() bool {
	return c.Tag != "" && c.Slots > 0
}

// Validate rejects invalid supplied values without requiring apply-only settings.
// Missing configuration leaves mutation unavailable without preventing cleanup.
func (c RuntimeConfig) Validate() error {
	if c.ReconcileTaskQueue != "" && !runtimeQueuePattern.MatchString(c.ReconcileTaskQueue) {
		return fmt.Errorf("invalid private ingress reconciliation task queue")
	}
	if c.Gateway.Tag != "" {
		tag, ok := strings.CutPrefix(c.Gateway.Tag, "tag:")
		if !ok || tag == "" || len(validation.IsDNS1123Label(tag)) > 0 {
			return fmt.Errorf("invalid private ingress gateway tag")
		}
	}
	if c.Gateway.Slots < 0 || c.Gateway.Slots > maxGatewaySlots {
		return fmt.Errorf("private ingress gateway slots must be between 1 and %d, or zero when unconfigured", maxGatewaySlots)
	}
	if c.Gateway.APIURL != "" {
		parsed, err := url.Parse(c.Gateway.APIURL)
		if err != nil || parsed.Host == "" {
			return fmt.Errorf("invalid private ingress Tailscale API URL")
		}
		if err := tailscaleapi.RequireTLS(parsed); err != nil {
			return fmt.Errorf("invalid private ingress Tailscale API URL: %w", err)
		}
	}
	return nil
}

// MutationReady reports configuration readiness for apply and rotation only.
// Callers must independently verify clients and admission integration readiness.
// Observe and Delete must not depend on this result.
func (c RuntimeConfig) MutationReady() bool {
	return c.ProviderMutationsEnabled && c.Validate() == nil && c.ReconcileTaskQueue != "" && c.Gateway.Ready()
}
