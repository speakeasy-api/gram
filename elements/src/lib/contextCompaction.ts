import type { ModelMessage } from "ai";

/**
 * Fraction-of-limit at which compaction kicks in. Below this, messages pass
 * through untouched; above this, oldest non-system turns are dropped until
 * the estimated token count is back under the threshold.
 */
export const DEFAULT_COMPACTION_FRACTION = 0.7;

/**
 * Number of most-recent messages preserved verbatim, even if the conversation
 * is already over the limit. Ensures the assistant always has the latest turn
 * and its immediate predecessor.
 */
export const DEFAULT_KEEP_RECENT = 4;

/**
 * Conservative fallback when we encounter a model we haven't mapped — big
 * enough to be useful for unknown models, small enough to still trigger
 * compaction before hitting upstream 400s.
 */
export const DEFAULT_CONTEXT_LIMIT = 200_000;

/**
 * Known input-token ceilings per model (nominal upstream maximum). Values are
 * kept intentionally conservative; users can override via
 * ElementsConfig.contextCompaction.maxTokens if needed.
 */
const MODEL_CONTEXT_LIMITS: Record<string, number> = {
  // Anthropic (1M tier where available, else 200K)
  "anthropic/claude-opus-4.6": 1_000_000,
  "anthropic/claude-opus-4.5": 1_000_000,
  "anthropic/claude-opus-4.1": 200_000,
  "anthropic/claude-sonnet-4.6": 1_000_000,
  "anthropic/claude-sonnet-4.5": 1_000_000,
  "anthropic/claude-sonnet-4": 200_000,
  "anthropic/claude-haiku-4.5": 200_000,

  // OpenAI
  "openai/gpt-5.4": 400_000,
  "openai/gpt-5.4-mini": 400_000,
  "openai/gpt-5.1": 400_000,
  "openai/gpt-5.1-codex": 400_000,
  "openai/gpt-5": 400_000,
  "openai/gpt-4.1": 1_000_000,
  "openai/o4-mini": 200_000,
  "openai/o3": 200_000,

  // Google
  "google/gemini-3.1-pro-preview": 1_000_000,
  "google/gemini-2.5-pro": 1_000_000,
  "google/gemini-2.5-flash": 1_000_000,

  // Others
  "deepseek/deepseek-r1": 128_000,
  "deepseek/deepseek-v3.2": 128_000,
  "meta-llama/llama-4-maverick": 1_000_000,
  "x-ai/grok-4": 256_000,
  "qwen/qwen3-coder": 256_000,
  "moonshotai/kimi-k2.5": 128_000,
  "mistralai/mistral-medium-3.1": 128_000,
  "mistralai/codestral-2508": 256_000,
  "mistralai/devstral-small": 128_000,
};

/**
 * Returns the input-token ceiling for a given OpenRouter model id, or
 * DEFAULT_CONTEXT_LIMIT if unknown.
 */
export function getModelContextLimit(modelId: string): number {
  return MODEL_CONTEXT_LIMITS[modelId] ?? DEFAULT_CONTEXT_LIMIT;
}

/**
 * Rough input-token estimate using a chars/4 heuristic on the JSON serialized
 * conversation. Tokens-per-char varies by model and content, but a chars/4
 * heuristic matches OpenAI's rule-of-thumb within ~15% for English prose and
 * is deterministic + zero-cost — good enough to trigger compaction.
 */
export function estimateTokens(messages: ModelMessage[]): number {
  const serialized = JSON.stringify(messages);
  return Math.ceil(serialized.length / 4);
}

export interface CompactionResult {
  messages: ModelMessage[];
  droppedCount: number;
  estimatedTokensBefore: number;
  estimatedTokensAfter: number;
}

/**
 * Drops oldest non-system messages until the estimated token count is under
 * maxTokens. Always preserves the last `keepRecent` messages and any system
 * role messages. When any messages are dropped, prepends a synthetic assistant
 * note so the model knows earlier context was elided.
 */
export function compactBySlidingWindow(
  messages: ModelMessage[],
  maxTokens: number,
  keepRecent: number = DEFAULT_KEEP_RECENT,
): CompactionResult {
  const estimatedTokensBefore = estimateTokens(messages);

  if (estimatedTokensBefore <= maxTokens || messages.length <= keepRecent) {
    return {
      messages,
      droppedCount: 0,
      estimatedTokensBefore,
      estimatedTokensAfter: estimatedTokensBefore,
    };
  }

  const systemMessages = messages.filter((m) => m.role === "system");
  const nonSystem = messages.filter((m) => m.role !== "system");

  // Group consecutive `tool` messages with the assistant message that
  // precedes them. OpenAI-compatible providers require every tool-result
  // message to be immediately preceded by the assistant message holding its
  // tool_calls — splitting these produces an invalid conversation that
  // providers reject with a 400. Grouping ensures we drop or keep the
  // full assistant+tools unit atomically.
  const groups: ModelMessage[][] = [];
  for (const m of nonSystem) {
    if (m.role === "tool" && groups.length > 0) {
      groups[groups.length - 1]!.push(m);
    } else {
      groups.push([m]);
    }
  }

  // Reserve the trailing groups that together contain at least `keepRecent`
  // messages. Using groups (not raw messages) keeps assistant+tool pairs
  // intact at the boundary between retained and dropped.
  let recentMsgCount = 0;
  let recentStart = groups.length;
  while (recentStart > 0 && recentMsgCount < keepRecent) {
    recentStart -= 1;
    recentMsgCount += groups[recentStart]!.length;
  }
  const recentGroups = groups.slice(recentStart);
  const droppableGroups = groups.slice(0, recentStart);

  let droppedCount = 0;
  let working = [
    ...systemMessages,
    ...droppableGroups.flat(),
    ...recentGroups.flat(),
  ];

  while (droppableGroups.length > 0 && estimateTokens(working) > maxTokens) {
    const droppedGroup = droppableGroups.shift()!;
    droppedCount += droppedGroup.length;
    working = [
      ...systemMessages,
      ...droppableGroups.flat(),
      ...recentGroups.flat(),
    ];
  }

  if (droppedCount > 0) {
    const marker: ModelMessage = {
      role: "assistant",
      content: `[${droppedCount} earlier message${
        droppedCount === 1 ? "" : "s"
      } omitted to stay under context length. If the user asks about them, say you no longer have that context and suggest they restate the relevant details.]`,
    };
    working = [
      ...systemMessages,
      marker,
      ...droppableGroups.flat(),
      ...recentGroups.flat(),
    ];
  }

  return {
    messages: working,
    droppedCount,
    estimatedTokensBefore,
    estimatedTokensAfter: estimateTokens(working),
  };
}

export interface CompactionOptions {
  /** Override the model's nominal input ceiling. */
  maxTokens?: number;
  /** Fraction of maxTokens at which compaction kicks in. */
  compactAtFraction?: number;
  /** Most-recent messages preserved verbatim. */
  keepRecent?: number;
}

/**
 * Convenience wrapper that picks the model ceiling, applies compactAtFraction,
 * and runs compactBySlidingWindow. Returns the (possibly unchanged) messages
 * plus diagnostics.
 */
export function compactForModel(
  messages: ModelMessage[],
  modelId: string,
  opts: CompactionOptions = {},
): CompactionResult {
  const ceiling = opts.maxTokens ?? getModelContextLimit(modelId);
  const fraction = opts.compactAtFraction ?? DEFAULT_COMPACTION_FRACTION;
  const limit = Math.floor(ceiling * fraction);
  return compactBySlidingWindow(messages, limit, opts.keepRecent);
}
