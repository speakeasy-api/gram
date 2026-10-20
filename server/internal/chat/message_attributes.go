package chat

import "strings"

// messageAttributes snapshots source-message dimensions independently of the
// classifier metadata on a sensor reading. Unknown facts stay absent.
func messageAttributes(model, provider, department string) map[string]string {
	var attributes map[string]string
	model = knownMessageAttribute(model)
	setReadingAttribute(&attributes, "model_name", model)
	setReadingAttribute(&attributes, "model_family", messageModelFamily(model))
	setReadingAttribute(&attributes, "model_provider", messageModelProvider(provider))
	setReadingAttribute(&attributes, "department", knownMessageAttribute(department))
	return attributes
}

func knownMessageAttribute(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "unknown", "<unset>":
		return ""
	default:
		return value
	}
}

// messageModelFamily identifies the model's vendor, never its serving route.
// Unrecognized namespaces and deployment aliases do not establish a vendor.
func messageModelFamily(model string) string {
	model = strings.ToLower(model)
	for {
		namespace, name, ok := strings.Cut(model, "/")
		if !ok {
			break
		}
		if name == "" {
			return ""
		}
		switch namespace {
		case "anthropic", "openai", "google", "deepseek", "cohere", "nvidia", "ai21", "amazon", "minimax":
			return namespace
		case "meta-llama", "meta":
			return "meta"
		case "mistralai", "mistral":
			return "mistral"
		case "qwen", "alibaba":
			return "alibaba"
		case "x-ai", "xai":
			return "xai"
		case "moonshotai":
			return "moonshot"
		case "openrouter", "bedrock":
			model = name
		default:
			return ""
		}
	}
	// Bedrock cross-region IDs retain the vendor after a geography prefix.
	for _, prefix := range []string{"us.", "eu.", "apac.", "global."} {
		model = strings.TrimPrefix(model, prefix)
	}
	switch {
	case strings.HasPrefix(model, "claude-"), strings.HasPrefix(model, "anthropic.claude-"):
		return "anthropic"
	case strings.HasPrefix(model, "gpt-"), strings.HasPrefix(model, "chatgpt-"),
		model == "o1", strings.HasPrefix(model, "o1-"), model == "o3", strings.HasPrefix(model, "o3-"), model == "o4", strings.HasPrefix(model, "o4-"):
		return "openai"
	case strings.HasPrefix(model, "gemini-"):
		return "google"
	case strings.HasPrefix(model, "mistral-"), strings.HasPrefix(model, "mixtral-"):
		return "mistral"
	case strings.HasPrefix(model, "deepseek-"):
		return "deepseek"
	default:
		return ""
	}
}

// messageModelProvider normalizes reported serving-provider aliases. A model
// family is never evidence that its vendor actually served the request.
func messageModelProvider(provider string) string {
	provider = strings.ToLower(knownMessageAttribute(provider))
	switch provider {
	case "amazon bedrock", "aws bedrock", "amazon-bedrock", "aws-bedrock":
		return "bedrock"
	case "open ai", "open-ai":
		return "openai"
	case "open router", "open-router":
		return "openrouter"
	default:
		return provider
	}
}
