import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * The type of hook event
 */
export declare const HookEventName: {
  readonly SessionStart: "SessionStart";
  readonly ConfigChange: "ConfigChange";
  readonly PreToolUse: "PreToolUse";
  readonly PostToolUse: "PostToolUse";
  readonly PostToolUseFailure: "PostToolUseFailure";
  readonly UserPromptSubmit: "UserPromptSubmit";
  readonly Stop: "Stop";
  readonly SessionEnd: "SessionEnd";
  readonly Notification: "Notification";
};
/**
 * The type of hook event
 */
export type HookEventName = ClosedEnum<typeof HookEventName>;
/**
 * Unified payload for all Claude Code hook events
 */
export type ClaudeHookPayload = {
  /**
   * Additional hook-specific data
   */
  additionalData?:
    | {
        [k: string]: any;
      }
    | undefined;
  /**
   * The working directory when the event fired
   */
  cwd?: string | undefined;
  /**
   * The error from the tool (PostToolUseFailure only)
   */
  error?: any | undefined;
  /**
   * The type of hook event
   */
  hookEventName: HookEventName;
  /**
   * Whether the failure was caused by user interruption (PostToolUseFailure only)
   */
  isInterrupt?: boolean | undefined;
  /**
   * Claude's final response text (Stop only)
   */
  lastAssistantMessage?: string | undefined;
  /**
   * Notification message text (Notification only)
   */
  message?: string | undefined;
  /**
   * The model identifier (SessionStart, Stop)
   */
  model?: string | undefined;
  /**
   * Type of notification: permission_prompt, idle_prompt, auth_success, elicitation_dialog (Notification only)
   */
  notificationType?: string | undefined;
  /**
   * The user's prompt text (UserPromptSubmit only)
   */
  prompt?: string | undefined;
  /**
   * Why the session ended (SessionEnd only)
   */
  reason?: string | undefined;
  /**
   * The Claude Code session ID
   */
  sessionId?: string | undefined;
  /**
   * How the session started: startup, resume, clear, compact (SessionStart only)
   */
  source?: string | undefined;
  /**
   * Whether a stop hook continuation is active (Stop only)
   */
  stopHookActive?: boolean | undefined;
  /**
   * Notification title (Notification only)
   */
  title?: string | undefined;
  /**
   * The input to the tool
   */
  toolInput?: any | undefined;
  /**
   * The name of the tool (for tool-related events)
   */
  toolName?: string | undefined;
  /**
   * The response from the tool (PostToolUse only)
   */
  toolResponse?: any | undefined;
  /**
   * The unique ID for this tool use
   */
  toolUseId?: string | undefined;
  /**
   * Path to the conversation transcript file
   */
  transcriptPath?: string | undefined;
  /**
   * Email of the authenticated user from the Speakeasy device agent, if available
   */
  userEmail?: string | undefined;
};
/** @internal */
export declare const HookEventName$outboundSchema: z.ZodMiniEnum<
  typeof HookEventName
>;
/** @internal */
export type ClaudeHookPayload$Outbound = {
  additional_data?:
    | {
        [k: string]: any;
      }
    | undefined;
  cwd?: string | undefined;
  error?: any | undefined;
  hook_event_name: string;
  is_interrupt?: boolean | undefined;
  last_assistant_message?: string | undefined;
  message?: string | undefined;
  model?: string | undefined;
  notification_type?: string | undefined;
  prompt?: string | undefined;
  reason?: string | undefined;
  session_id?: string | undefined;
  source?: string | undefined;
  stop_hook_active?: boolean | undefined;
  title?: string | undefined;
  tool_input?: any | undefined;
  tool_name?: string | undefined;
  tool_response?: any | undefined;
  tool_use_id?: string | undefined;
  transcript_path?: string | undefined;
  user_email?: string | undefined;
};
/** @internal */
export declare const ClaudeHookPayload$outboundSchema: z.ZodMiniType<
  ClaudeHookPayload$Outbound,
  ClaudeHookPayload
>;
export declare function claudeHookPayloadToJSON(
  claudeHookPayload: ClaudeHookPayload,
): string;
//# sourceMappingURL=claudehookpayload.d.ts.map
