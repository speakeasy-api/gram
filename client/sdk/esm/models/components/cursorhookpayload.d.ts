import * as z from "zod/v4-mini";
/**
 * Payload for Cursor hook events
 */
export type CursorHookPayload = {
  /**
   * Additional hook-specific data
   */
  additionalData?:
    | {
        [k: string]: any;
      }
    | undefined;
  /**
   * Tokens read from cache (stop, afterAgentResponse)
   */
  cacheReadTokens?: number | undefined;
  /**
   * Tokens written to cache (stop, afterAgentResponse)
   */
  cacheWriteTokens?: number | undefined;
  /**
   * Command string for command-based MCP servers (beforeMCPExecution / afterMCPExecution only)
   */
  command?: string | undefined;
  /**
   * The composer mode, e.g. agent (beforeSubmitPrompt only)
   */
  composerMode?: string | undefined;
  /**
   * The Cursor conversation ID
   */
  conversationId?: string | undefined;
  /**
   * The Cursor IDE version
   */
  cursorVersion?: string | undefined;
  /**
   * Execution duration in milliseconds, excluding approval wait time (afterMCPExecution only)
   */
  duration?: number | undefined;
  /**
   * Duration in milliseconds for the thinking block (afterAgentThought only)
   */
  durationMs?: number | undefined;
  /**
   * The error from the tool (postToolUseFailure only)
   */
  error?: any | undefined;
  /**
   * The Cursor generation ID
   */
  generationId?: string | undefined;
  /**
   * The type of hook event (e.g. beforeSubmitPrompt, stop, afterAgentResponse, afterAgentThought, preToolUse, postToolUse, postToolUseFailure, beforeMCPExecution, afterMCPExecution)
   */
  hookEventName: string;
  /**
   * Total input tokens used (stop, afterAgentResponse)
   */
  inputTokens?: number | undefined;
  /**
   * Whether the failure was caused by user interruption
   */
  isInterrupt?: boolean | undefined;
  /**
   * Number of agentic loops executed (stop only)
   */
  loopCount?: number | undefined;
  /**
   * The model being used
   */
  model?: string | undefined;
  /**
   * Total output tokens used (stop, afterAgentResponse)
   */
  outputTokens?: number | undefined;
  /**
   * The user's prompt text (beforeSubmitPrompt only)
   */
  prompt?: string | undefined;
  /**
   * JSON-encoded string of the MCP tool response (afterMCPExecution only)
   */
  resultJson?: string | undefined;
  /**
   * The session ID from Cursor
   */
  sessionId?: string | undefined;
  /**
   * Completion status, e.g. completed (stop only)
   */
  status?: string | undefined;
  /**
   * The assistant's response text (afterAgentResponse) or thinking text (afterAgentThought)
   */
  text?: string | undefined;
  /**
   * The input to the tool
   */
  toolInput?: any | undefined;
  /**
   * The name of the tool
   */
  toolName?: string | undefined;
  /**
   * The response from the tool (postToolUse only)
   */
  toolResponse?: any | undefined;
  /**
   * The unique ID for this tool use
   */
  toolUseId?: string | undefined;
  /**
   * Path to the conversation transcript JSONL file
   */
  transcriptPath?: string | undefined;
  /**
   * URL of the MCP server (beforeMCPExecution / afterMCPExecution, URL-based servers only)
   */
  url?: string | undefined;
  /**
   * Email of the authenticated Cursor user, if available
   */
  userEmail?: string | undefined;
};
/** @internal */
export type CursorHookPayload$Outbound = {
  additional_data?:
    | {
        [k: string]: any;
      }
    | undefined;
  cache_read_tokens?: number | undefined;
  cache_write_tokens?: number | undefined;
  command?: string | undefined;
  composer_mode?: string | undefined;
  conversation_id?: string | undefined;
  cursor_version?: string | undefined;
  duration?: number | undefined;
  duration_ms?: number | undefined;
  error?: any | undefined;
  generation_id?: string | undefined;
  hook_event_name: string;
  input_tokens?: number | undefined;
  is_interrupt?: boolean | undefined;
  loop_count?: number | undefined;
  model?: string | undefined;
  output_tokens?: number | undefined;
  prompt?: string | undefined;
  result_json?: string | undefined;
  session_id?: string | undefined;
  status?: string | undefined;
  text?: string | undefined;
  tool_input?: any | undefined;
  tool_name?: string | undefined;
  tool_response?: any | undefined;
  tool_use_id?: string | undefined;
  transcript_path?: string | undefined;
  url?: string | undefined;
  user_email?: string | undefined;
};
/** @internal */
export declare const CursorHookPayload$outboundSchema: z.ZodMiniType<
  CursorHookPayload$Outbound,
  CursorHookPayload
>;
export declare function cursorHookPayloadToJSON(
  cursorHookPayload: CursorHookPayload,
): string;
//# sourceMappingURL=cursorhookpayload.d.ts.map
