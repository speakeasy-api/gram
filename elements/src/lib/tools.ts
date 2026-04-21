import type { ToolsFilter } from "@/types";
import {
  AssistantToolProps,
  Tool,
  makeAssistantTool,
} from "@assistant-ui/react";
import { JSONSchema7, ToolSet, type ToolCallOptions } from "ai";
import { FC } from "react";
import z from "zod";

/**
 * Converts from assistant-ui tool format to the AI SDK tool shape
 */
export const toAISDKTools = (tools: Record<string, Tool>) => {
  return Object.fromEntries(
    Object.entries(tools).map(([name, tool]) => [
      name,
      {
        ...(tool.description ? { description: tool.description } : undefined),
        parameters: (tool.parameters instanceof z.ZodType
          ? z.toJSONSchema(tool.parameters)
          : tool.parameters) as JSONSchema7,
      },
    ]),
  );
};

/**
 * Returns only frontend tools that are enabled
 */
export const getEnabledTools = (tools: Record<string, Tool>) => {
  return Object.fromEntries(
    Object.entries(tools).filter(
      ([, tool]) => !tool.disabled && tool.type !== "backend",
    ),
  );
};

/**
 * A frontend tool is a tool that is defined by the user and can be used in the chat.
 */
export type FrontendTool<TArgs extends Record<string, unknown>, TResult> = FC<
  AssistantToolProps<TArgs, TResult>
> & {
  unstable_tool: AssistantToolProps<TArgs, TResult>;
};

/**
 * Module-level approval config that gets set by ElementsProvider at runtime.
 * This allows defineFrontendTool to check approval status during execute.
 */
let approvalConfig: {
  helpers: ApprovalHelpers;
  requiresApproval: (toolName: string) => boolean;
} | null = null;

/**
 * Sets the approval configuration. Called by ElementsProvider.
 */
export function setFrontendToolApprovalConfig(
  helpers: ApprovalHelpers,
  toolsRequiringApproval: ToolsFilter,
): void {
  const requiresApproval = createRequiresApprovalFn(toolsRequiringApproval);
  approvalConfig = {
    helpers,
    requiresApproval,
  };
}

/**
 * Clears the approval configuration. Called when ElementsProvider unmounts.
 */
export function clearFrontendToolApprovalConfig(): void {
  approvalConfig = null;
}

/**
 * Creates a function that checks if a tool requires approval.
 * Handles both array and function-based configurations.
 */
function createRequiresApprovalFn(
  toolsRequiringApproval: ToolsFilter | undefined,
): (toolName: string) => boolean {
  if (!toolsRequiringApproval) {
    return () => false;
  }

  if (typeof toolsRequiringApproval === "function") {
    return (toolName: string) => toolsRequiringApproval({ toolName });
  }

  const approvalSet = new Set(toolsRequiringApproval);
  return (toolName: string) => approvalSet.has(toolName);
}

/**
 * Make a frontend tool
 */
export const defineFrontendTool = <
  TArgs extends Record<string, unknown>,
  TResult,
>(
  tool: Tool,
  name: string,
): FrontendTool<TArgs, TResult> => {
  type ToolExecutionContext = Parameters<
    NonNullable<Tool<Record<string, unknown>, void>["execute"]>
  >[1];
  return makeAssistantTool({
    ...tool,
    execute: async (args: TArgs, context: ToolExecutionContext) => {
      // Check if this tool requires approval at runtime
      if (approvalConfig?.requiresApproval(name)) {
        const { helpers } = approvalConfig;
        const toolCallId = context.toolCallId ?? "";

        // Check if already approved (user chose "Approve always" previously)
        if (!helpers.isToolApproved(name)) {
          const approved = await helpers.requestApproval(
            name,
            toolCallId,
            args,
          );

          if (!approved) {
            return {
              content: [
                {
                  type: "text",
                  text: `Tool "${name}" execution was denied by the user. Please acknowledge this and continue without using this tool's result.`,
                },
              ],
              isError: true,
            } as TResult;
          }
        }
      }

      return tool.execute?.(args, context);
    },
    toolName: name,
  } as AssistantToolProps<TArgs, TResult>);
};

/**
 * Helpers for requesting and tracking tool approval state.
 */
export interface ApprovalHelpers {
  requestApproval: (
    toolName: string,
    toolCallId: string,
    args: unknown,
  ) => Promise<boolean>;
  isToolApproved: (toolName: string) => boolean;
  whitelistTool: (toolName: string) => void;
}

/**
 * Default head/tail split (bytes) when a tool result exceeds the cap. Head keeps
 * early context (e.g. the preamble of a log query); tail keeps the most recent
 * lines, which are usually the most relevant.
 */
const BYTE_CAP_HEAD_FRACTION = 0.9;

/**
 * Truncates a single string to maxBytes using a head + tail preserving strategy
 * when it exceeds the cap. Returns the original string when under the cap.
 */
export function truncateTextToByteCap(text: string, maxBytes: number): string {
  if (maxBytes <= 0) return text;
  const original = text;
  // Work in UTF-8 bytes to match what OpenRouter counts.
  const encoded = new TextEncoder().encode(original);
  if (encoded.byteLength <= maxBytes) return original;

  // Reserve room for the notice up-front so final output stays under maxBytes.
  // Without this deduction, output would be head + notice + tail ≈ maxBytes
  // + ~100 bytes, which silently overshoots the cap.
  const notice = `\n\n[…tool output truncated from ${encoded.byteLength} bytes to ${maxBytes}; ask a narrower question to see more…]\n\n`;
  const noticeBytes = new TextEncoder().encode(notice).byteLength;
  const availableBytes = Math.max(0, maxBytes - noticeBytes);

  const headBytes = Math.max(
    0,
    Math.floor(availableBytes * BYTE_CAP_HEAD_FRACTION),
  );
  const tailBytes = Math.max(0, availableBytes - headBytes);
  const decoder = new TextDecoder("utf-8", { fatal: false });
  const head = decoder.decode(encoded.slice(0, headBytes));
  const tail =
    tailBytes > 0
      ? decoder.decode(encoded.slice(encoded.byteLength - tailBytes))
      : "";

  return tail ? `${head}${notice}${tail}` : `${head}${notice}`;
}

/**
 * Walks the shape returned by MCP/AI-SDK tool executors and truncates any
 * over-sized text payload in place. Handles:
 *   - plain strings
 *   - { content: Array<{ type, text?, ... }>, isError? }
 * Other shapes pass through untouched.
 */
export function capToolResultBytes(result: unknown, maxBytes: number): unknown {
  if (maxBytes <= 0) return result;

  if (typeof result === "string") {
    return truncateTextToByteCap(result, maxBytes);
  }

  if (result && typeof result === "object" && "content" in result) {
    const r = result as {
      content?: unknown;
      isError?: boolean;
      [k: string]: unknown;
    };
    if (Array.isArray(r.content)) {
      const cappedContent = r.content.map((chunk) => {
        if (
          chunk &&
          typeof chunk === "object" &&
          (chunk as { type?: unknown }).type === "text" &&
          typeof (chunk as { text?: unknown }).text === "string"
        ) {
          return {
            ...(chunk as Record<string, unknown>),
            text: truncateTextToByteCap(
              (chunk as { text: string }).text,
              maxBytes,
            ),
          };
        }
        return chunk;
      });
      return { ...r, content: cappedContent };
    }
  }

  return result;
}

/**
 * Wraps tools so that oversized results are truncated before they reach the
 * conversation history. Tools whose result fits under the cap pass through
 * untouched. Composes cleanly before or after wrapToolsWithApproval.
 */
export function wrapToolsWithByteCap(
  tools: ToolSet,
  maxBytes: number | undefined,
): ToolSet {
  if (!maxBytes || maxBytes <= 0) {
    return tools;
  }

  return Object.fromEntries(
    Object.entries(tools).map(([name, tool]) => {
      const originalExecute = tool.execute;
      if (!originalExecute) {
        return [name, tool];
      }

      return [
        name,
        {
          ...tool,
          execute: async (args: unknown, options?: ToolCallOptions) => {
            const result = await originalExecute(
              args,
              options as Parameters<typeof originalExecute>[1],
            );
            return capToolResultBytes(result, maxBytes);
          },
        },
      ];
    }),
  ) as ToolSet;
}

/**
 * Wraps tools with approval logic based on the approval config.
 */
export function wrapToolsWithApproval(
  tools: ToolSet,
  toolsRequiringApproval: ToolsFilter | undefined,
  approvalHelpers: ApprovalHelpers,
): ToolSet {
  if (!toolsRequiringApproval) {
    return tools;
  }

  // Handle empty array case
  if (
    Array.isArray(toolsRequiringApproval) &&
    toolsRequiringApproval.length === 0
  ) {
    return tools;
  }

  const requiresApproval = createRequiresApprovalFn(toolsRequiringApproval);

  return Object.fromEntries(
    Object.entries(tools).map(([name, tool]) => {
      if (!requiresApproval(name)) {
        return [name, tool];
      }

      const originalExecute = tool.execute;
      if (!originalExecute) {
        return [name, tool];
      }

      return [
        name,
        {
          ...tool,
          execute: async (args: unknown, options?: ToolCallOptions) => {
            const opts = (options ?? {}) as Parameters<
              typeof originalExecute
            >[1];
            // Extract toolCallId from options
            const toolCallId =
              (opts as { toolCallId?: string }).toolCallId ?? "";

            // Check if already approved (user chose "Approve always" previously)
            if (approvalHelpers.isToolApproved(name)) {
              return originalExecute(
                args,
                opts as Parameters<typeof originalExecute>[1],
              );
            }

            // Request approval using the actual toolCallId from the stream
            const approved = await approvalHelpers.requestApproval(
              name,
              toolCallId,
              args,
            );

            if (!approved) {
              return {
                content: [
                  {
                    type: "text",
                    text: `Tool "${name}" execution was denied by the user. Please acknowledge this and continue without using this tool's result.`,
                  },
                ],
                isError: true,
              };
            }

            // Note: Tool is marked as approved via the UI when user clicks "Approve always"
            // (handled in tool-fallback.tsx via markToolApproved)

            return originalExecute(
              args,
              opts as Parameters<typeof originalExecute>[1],
            );
          },
        },
      ];
    }),
  ) as ToolSet;
}
