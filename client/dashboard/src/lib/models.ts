export type AvailableModel = {
  value: string;
  label: string;
};

export const AVAILABLE_MODELS: AvailableModel[] = [
  { value: "anthropic/claude-opus-4.8", label: "Claude Opus 4.8 (Expensive)" },
  { value: "anthropic/claude-opus-4.7", label: "Claude Opus 4.7 (Expensive)" },
  { value: "anthropic/claude-sonnet-4.6", label: "Claude Sonnet 4.6" },
  { value: "anthropic/claude-sonnet-4.5", label: "Claude Sonnet 4.5" },
  { value: "anthropic/claude-opus-4.6", label: "Claude Opus 4.6 (Expensive)" },
  { value: "anthropic/claude-opus-4.5", label: "Claude Opus 4.5 (Expensive)" },
  { value: "anthropic/claude-haiku-4.5", label: "Claude Haiku 4.5" },
  { value: "anthropic/claude-sonnet-4", label: "Claude Sonnet 4" },
  { value: "openai/gpt-5.5", label: "GPT-5.5" },
  { value: "openai/gpt-5.5-pro", label: "GPT-5.5 Pro (Expensive)" },
  { value: "openai/gpt-5.4", label: "GPT-5.4" },
  { value: "openai/gpt-5.4-mini", label: "GPT-5.4 Mini" },
  { value: "openai/gpt-5.4-nano", label: "GPT-5.4 Nano" },
  { value: "openai/gpt-5.3-codex", label: "GPT-5.3 Codex" },
  { value: "openai/gpt-5.1", label: "GPT-5.1" },
  { value: "openai/gpt-5", label: "GPT-5" },
  { value: "openai/gpt-4.1", label: "GPT-4.1" },
  { value: "openai/o4-mini", label: "o4-mini" },
  { value: "openai/o3", label: "o3" },
  { value: "google/gemini-3.5-flash", label: "Gemini 3.5 Flash" },
  { value: "google/gemini-3.1-pro-preview", label: "Gemini 3.1 Pro Preview" },
  { value: "google/gemini-3.1-flash-lite", label: "Gemini 3.1 Flash Lite" },
  { value: "google/gemini-2.5-pro", label: "Gemini 2.5 Pro" },
  { value: "google/gemini-2.5-flash", label: "Gemini 2.5 Flash" },
  { value: "deepseek/deepseek-v4-pro", label: "DeepSeek V4 Pro" },
  { value: "deepseek/deepseek-v4-flash", label: "DeepSeek V4 Flash" },
  { value: "deepseek/deepseek-v3.2", label: "DeepSeek V3.2" },
  { value: "deepseek/deepseek-r1", label: "DeepSeek R1" },
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

const DEFAULT_MODEL = "anthropic/claude-sonnet-4.6";
