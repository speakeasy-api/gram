import type { Model } from "@/elements";

export type AvailableModel = {
  value: string;
  label: string;
  // Premium-priced models get an "Expensive" badge in model pickers.
  expensive?: boolean;
};

export const AVAILABLE_MODELS: AvailableModel[] = [
  {
    value: "anthropic/claude-fable-5",
    label: "Claude Fable 5",
    expensive: true,
  },
  { value: "anthropic/claude-sonnet-5", label: "Claude Sonnet 5" },
  {
    value: "anthropic/claude-opus-4.8",
    label: "Claude Opus 4.8",
    expensive: true,
  },
  {
    value: "anthropic/claude-opus-4.7",
    label: "Claude Opus 4.7",
    expensive: true,
  },
  { value: "anthropic/claude-sonnet-4.6", label: "Claude Sonnet 4.6" },
  { value: "anthropic/claude-sonnet-4.5", label: "Claude Sonnet 4.5" },
  {
    value: "anthropic/claude-opus-4.6",
    label: "Claude Opus 4.6",
    expensive: true,
  },
  {
    value: "anthropic/claude-opus-4.5",
    label: "Claude Opus 4.5",
    expensive: true,
  },
  { value: "anthropic/claude-haiku-4.5", label: "Claude Haiku 4.5" },
  { value: "openai/gpt-5.6-sol", label: "GPT-5.6 Sol", expensive: true },
  { value: "openai/gpt-5.6-terra", label: "GPT-5.6 Terra" },
  { value: "openai/gpt-5.6-luna", label: "GPT-5.6 Luna" },
  { value: "openai/gpt-5.5", label: "GPT-5.5" },
  { value: "openai/gpt-5.5-pro", label: "GPT-5.5 Pro", expensive: true },
  { value: "openai/gpt-5.4", label: "GPT-5.4" },
  { value: "openai/gpt-5.4-mini", label: "GPT-5.4 Mini" },
  { value: "openai/gpt-5.4-nano", label: "GPT-5.4 Nano" },
  { value: "openai/gpt-5.3-codex", label: "GPT-5.3 Codex" },
  { value: "openai/gpt-5.1", label: "GPT-5.1" },
  { value: "openai/gpt-5", label: "GPT-5" },
  { value: "google/gemini-3.5-flash", label: "Gemini 3.5 Flash" },
  { value: "google/gemini-3.1-pro-preview", label: "Gemini 3.1 Pro Preview" },
  { value: "google/gemini-3.1-flash-lite", label: "Gemini 3.1 Flash Lite" },
  { value: "deepseek/deepseek-v4-pro", label: "DeepSeek V4 Pro" },
  { value: "deepseek/deepseek-v4-flash", label: "DeepSeek V4 Flash" },
  { value: "deepseek/deepseek-v3.2", label: "DeepSeek V3.2" },
  { value: "meta-llama/llama-4-maverick", label: "Llama 4 Maverick" },
  { value: "x-ai/grok-4.3", label: "Grok 4.3" },
  { value: "x-ai/grok-4.20", label: "Grok 4.20" },
  { value: "qwen/qwen3.7-max", label: "Qwen3.7 Max" },
  { value: "qwen/qwen3-coder", label: "Qwen3 Coder" },
  { value: "moonshotai/kimi-k2.6", label: "Kimi K2.6" },
  { value: "moonshotai/kimi-k2.5", label: "Kimi K2.5" },
  { value: "mistralai/mistral-medium-3-5", label: "Mistral Medium 3.5" },
  { value: "mistralai/codestral-2508", label: "Codestral 2508" },
  { value: "mistralai/devstral-2512", label: "Devstral 2512" },
  { value: "mistralai/mistral-medium-3.1", label: "Mistral Medium 3.1" },
];

// Default model used across in-app chat surfaces (playground, MCP test chat,
// chat window) when the user has not picked one explicitly. Kept next to
// AVAILABLE_MODELS so the default is easy to discover and adjust.
export const DEFAULT_MODEL: Model = "anthropic/claude-sonnet-5";

// Default model assigned to newly created assistants (onboarding flow). Tracked
// separately from DEFAULT_MODEL so the assistant default can move independently
// of the general in-app chat default.
export const DEFAULT_ASSISTANT_MODEL: Model = "anthropic/claude-sonnet-5";
