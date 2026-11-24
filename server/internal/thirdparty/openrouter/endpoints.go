package openrouter

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/partition"
)

// EUBaseURL is OpenRouter's in-region host for the EU. OpenRouter processes
// requests sent there only with providers in the EU/EEA, the UK and
// Switzerland, and fails a request rather than leave the region when no such
// provider serves the model.
const EUBaseURL = "https://eu.openrouter.ai/api"

// OpenRouter's EU host does not list the rolling Opus alias. Keep its prior
// in-region model rather than sending EU traffic to the global host.
func modelForInferenceHost(baseURL, model string) string {
	if baseURL == EUBaseURL && model == claudeOpusLatest {
		return "anthropic/claude-opus-5"
	}
	return model
}

// Endpoints are the OpenRouter hosts and capabilities a partition uses.
type Endpoints struct {
	// Inference is the base URL for requests that carry prompts or that are
	// authenticated with an organization's own key: completions, embeddings,
	// model discovery, model endpoint metadata, generation lookups, key usage,
	// Decisions and System One. OpenRouter guardrails can restrict a key to one
	// region, so these requests must use the partition's host.
	Inference string

	// Management is the base URL for requests authenticated with the
	// provisioning key: creating and updating organization keys, reading a
	// key's usage by hash, and spend analytics. OpenRouter keeps account and
	// billing records on its global host, so every partition uses it.
	Management string

	// ServesDecisionModels reports whether OpenRouter serves Decisions and
	// System One models, such as Jev, on the inference host. None of them has
	// a provider in the EU.
	ServesDecisionModels bool
}

// EndpointsFor returns the OpenRouter endpoints for a partition. p must be a
// value accepted by partition.Parse, which every process validates at
// startup; any other value panics.
func EndpointsFor(p partition.Partition) Endpoints {
	switch p {
	case partition.US:
		return Endpoints{Inference: OpenRouterBaseURL, Management: OpenRouterBaseURL, ServesDecisionModels: true}
	case partition.EU:
		return Endpoints{Inference: EUBaseURL, Management: OpenRouterBaseURL, ServesDecisionModels: false}
	}

	panic(fmt.Sprintf("openrouter: no endpoints for partition %q", p))
}
