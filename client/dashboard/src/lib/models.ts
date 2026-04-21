export type AvailableModel = {
  value: string;
  label: string;
};

export const AVAILABLE_MODELS: AvailableModel[] = [
  { value: "anthropic/claude-opus-4.6", label: "Claude Opus 4.6 (Expensive)" },
  { value: "anthropic/claude-sonnet-4.6", label: "Claude Sonnet 4.6" },
  { value: "anthropic/claude-sonnet-4.5", label: "Claude Sonnet 4.5" },
  { value: "anthropic/claude-opus-4.5", label: "Claude Opus 4.5 (Expensive)" },
  { value: "anthropic/claude-haiku-4.5", label: "Claude Haiku 4.5" },
  { value: "anthropic/claude-opus-4.1", label: "Claude Opus 4.1 (Expensive)" },
  { value: "anthropic/claude-sonnet-4", label: "Claude Sonnet 4" },
  { value: "openai/gpt-5.4", label: "GPT-5.4" },
  { value: "openai/gpt-5.4-mini", label: "GPT-5.4 Mini" },
  { value: "openai/gpt-5.1", label: "GPT-5.1" },
  { value: "openai/gpt-5.1-codex", label: "GPT-5.1 Codex" },
  { value: "openai/gpt-5", label: "GPT-5" },
  { value: "openai/gpt-4.1", label: "GPT-4.1" },
  { value: "openai/o4-mini", label: "o4-mini" },
  { value: "openai/o3", label: "o3" },
  { value: "google/gemini-3.1-pro-preview", label: "Gemini 3.1 Pro Preview" },
  { value: "google/gemini-2.5-pro", label: "Gemini 2.5 Pro" },
  { value: "google/gemini-2.5-flash", label: "Gemini 2.5 Flash" },
  { value: "deepseek/deepseek-r1", label: "DeepSeek R1" },
  { value: "deepseek/deepseek-v3.2", label: "DeepSeek V3.2" },
  { value: "meta-llama/llama-4-maverick", label: "Llama 4 Maverick" },
  { value: "x-ai/grok-4", label: "Grok 4" },
  { value: "qwen/qwen3-coder", label: "Qwen3 Coder" },
  { value: "moonshotai/kimi-k2.5", label: "Kimi K2.5" },
  { value: "mistralai/mistral-medium-3.1", label: "Mistral Medium 3.1" },
  { value: "mistralai/codestral-2508", label: "Codestral 2508" },
  { value: "mistralai/devstral-small", label: "Devstral Small" },
];

export const DEFAULT_MODEL = "anthropic/claude-sonnet-4.6";
