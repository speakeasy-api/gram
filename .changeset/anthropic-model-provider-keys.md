---
"server": minor
"dashboard": patch
---

Projects can now use Anthropic API keys for Claude model completions. Anthropic keys are validated before encrypted storage and route supported Claude requests directly through Anthropic's OpenAI-compatible endpoint, while other model providers continue to use the project's OpenRouter or platform key.
