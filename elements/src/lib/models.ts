// List of openrouter models available to the user
// This list should be updated to match the model whitelist on the backend side.
export const MODELS = [
  "anthropic/claude-opus-4.6",
  "anthropic/claude-sonnet-4.5",
  "anthropic/claude-haiku-4.5",
  "anthropic/claude-sonnet-4",
  "anthropic/claude-opus-4.5",
  "openai/gpt-5.4",
  "openai/gpt-4o",
  "openai/gpt-4o-mini",
  "openai/gpt-5.1-codex",
  "openai/gpt-5.1",
  "openai/gpt-4.1",
  "google/gemini-2.5-pro",
  "google/gemini-3.1-pro-preview",
  "moonshotai/kimi-k2",
  "mistralai/mistral-medium-3",
  "mistralai/mistral-medium-3.1",
  "mistralai/codestral-2501",
] as const;
