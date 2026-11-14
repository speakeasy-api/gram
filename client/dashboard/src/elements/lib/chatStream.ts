import { compactForModel } from "@/elements/lib/contextCompaction";
import { describeStreamError } from "@/elements/lib/streamErrorMessage";
import {
  wrapToolsWithApproval,
  wrapToolsWithByteCap,
  type ApprovalHelpers,
} from "@/elements/lib/tools";
import type { ContextCompactionConfig } from "@/elements/types";
import { frontendTools as convertFrontendToolsToAISDKTools } from "@assistant-ui/react-ai-sdk";
import { createOpenRouter } from "@openrouter/ai-sdk-provider";
import {
  convertToModelMessages,
  createUIMessageStream,
  isStepCount,
  smoothStream,
  streamText,
  toUIMessageStream,
  type LanguageModel,
  type ToolSet,
  type UIMessage,
  type UIMessageChunk,
} from "ai";

type UIMessagePart = UIMessage["parts"][number];

/**
 * The chat model behind the built-in transport: an OpenRouter client pointed
 * at the Speakeasy API, which proxies completions to OpenRouter.
 */
export function createChatModel(options: {
  apiUrl: string;
  headers: Record<string, string>;
  modelId: string;
}): LanguageModel {
  const openRouter = createOpenRouter({
    baseURL: options.apiUrl,
    apiKey: "unused, but must be set",
    headers: options.headers,
  });
  return openRouter.chat(options.modelId) as LanguageModel;
}

/**
 * Combines MCP and frontend tools, gates the ones that need approval, and caps
 * oversized results so one greedy tool call (e.g. a wide log search) can't fill
 * the context window in a single step.
 */
export function buildChatTools(options: {
  mcpTools: ToolSet | undefined;
  frontendTools: Parameters<typeof convertFrontendToolsToAISDKTools>[0];
  toolsRequiringApproval: Parameters<typeof wrapToolsWithApproval>[1];
  approvalHelpers: ApprovalHelpers;
  maxOutputBytes: number | undefined;
}): ToolSet {
  const combinedTools: ToolSet = {
    ...options.mcpTools,
    ...convertFrontendToolsToAISDKTools(options.frontendTools),
  } as ToolSet;

  const approvedTools = wrapToolsWithApproval(
    combinedTools,
    options.toolsRequiringApproval,
    options.approvalHelpers,
  );

  return wrapToolsWithByteCap(approvedTools, options.maxOutputBytes);
}

/**
 * Cleans messages before sending to the model to work around an AI SDK bug.
 * Strips callProviderMetadata from all parts (AI SDK bug #9731)
 */
function cleanMessagesForModel(messages: UIMessage[]): UIMessage[] {
  return messages.map((message) => {
    const partsArray = message.parts;
    if (!Array.isArray(partsArray)) {
      return message;
    }

    // Strip `callProviderMetadata` from each part; everything else is kept.
    // `callProviderMetadata` is not declared on `UIMessagePart`, so we widen the
    // part to an indexable record just for the destructure.
    const cleanedParts = partsArray.map((part) => {
      const { callProviderMetadata: _omit, ...cleanPart } =
        part as UIMessagePart & { callProviderMetadata?: unknown };
      void _omit;
      return cleanPart as UIMessagePart;
    });

    return {
      ...message,
      parts: cleanedParts,
    };
  });
}

/**
 * Runs one chat turn against the model and returns the UI message stream that
 * useChat renders. Everything between the thread's UI messages and the stream
 * goes through here, so a dependency update that breaks message conversion,
 * tool calling, or stream encoding fails the tests for this function.
 */
export async function streamChatTurn(options: {
  messages: UIMessage[];
  model: LanguageModel;
  modelId: string;
  instructions: string;
  tools: ToolSet;
  abortSignal?: AbortSignal;
  contextCompaction?: ContextCompactionConfig;
  onStreamError: (error: unknown) => void;
}): Promise<ReadableStream<UIMessageChunk>> {
  const { messages, model, modelId, tools, contextCompaction } = options;

  // This works around AI SDK bug where these fields cause validation failures
  const cleanedMessages = cleanMessagesForModel(messages);
  // Filter out system messages from the UI state — the system prompt
  // is already provided via the `instructions:` parameter to streamText().
  // Without this, loaded chat history includes the system message which
  // gets sent alongside the `instructions:` param, causing duplication.
  const nonSystemMessages = cleanedMessages.filter((m) => m.role !== "system");
  const rawModelMessages = await convertToModelMessages(nonSystemMessages);

  // Auto-compact older turns if the estimated input is approaching
  // the model's context window. System prompt + last few turns are
  // always preserved. No-op when the conversation is small.
  const compaction = contextCompaction?.disabled
    ? {
        messages: rawModelMessages,
        droppedCount: 0,
        estimatedTokensBefore: 0,
        estimatedTokensAfter: 0,
      }
    : compactForModel(rawModelMessages, modelId, {
        maxTokens: contextCompaction?.maxTokens,
        compactAtFraction: contextCompaction?.compactAtFraction,
        keepRecent: contextCompaction?.keepRecent,
      });
  if (compaction.droppedCount > 0) {
    console.warn(
      `[elements] compacted ${compaction.droppedCount} older turn(s) from ${compaction.estimatedTokensBefore} → ${compaction.estimatedTokensAfter} est. tokens (model ${modelId})`,
    );
  }

  const result = streamText({
    instructions: options.instructions,
    model,
    messages: compaction.messages,
    tools,
    stopWhen: isStepCount(10),
    experimental_transform: smoothStream({ delayInMs: 15 }),
    abortSignal: options.abortSignal,
    onError: ({ error }) => options.onStreamError(error),
  });

  // This weird construction is necessary to get errors to propagate properly to assistant-ui.
  // `originalMessages` is required: without it, `handleUIMessageStreamFinish` injects a
  // fresh random messageId into every `start` chunk. On auto-resume that mismatches the
  // prior assistant message's id, so useChat pushes a new UIMessage carrying the snapshot
  // of the prior turn's parts — duplicating text and tool_calls into storage.
  //
  // onError: AI SDK masks errors by default; surface the friendly
  // credits prompt for 402, otherwise keep the masking intact.
  //
  // Both streams need it. Errors from the model call reach the UI through
  // toUIMessageStream, so its handler keeps that stream's default masked text;
  // createUIMessageStream's handler covers errors thrown by `execute` itself.
  return createUIMessageStream({
    execute: ({ writer }) => {
      writer.merge(
        toUIMessageStream({
          stream: result.stream,
          tools,
          onError: (error) =>
            describeStreamError(error) ?? "An error occurred.",
        }),
      );
    },
    originalMessages: messages,
    onError: (error) =>
      describeStreamError(error) ??
      "An error occurred while generating a response.",
  });
}
