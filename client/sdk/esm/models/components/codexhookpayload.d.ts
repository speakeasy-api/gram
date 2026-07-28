import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * The type of hook event
 */
export declare const CodexHookPayloadHookEventName: {
  readonly SessionStart: "SessionStart";
  readonly PreToolUse: "PreToolUse";
  readonly PermissionRequest: "PermissionRequest";
  readonly PostToolUse: "PostToolUse";
  readonly UserPromptSubmit: "UserPromptSubmit";
  readonly Stop: "Stop";
};
/**
 * The type of hook event
 */
export type CodexHookPayloadHookEventName = ClosedEnum<
  typeof CodexHookPayloadHookEventName
>;
/**
 * Payload for Codex hook events
 */
export type CodexHookPayload = {
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
   * The type of hook event
   */
  hookEventName: CodexHookPayloadHookEventName;
  /**
   * The final assistant message text for the turn (Stop only)
   */
  lastAssistantMessage?: string | undefined;
  /**
   * The model identifier
   */
  model?: string | undefined;
  /**
   * The type of permission being requested (PermissionRequest only)
   */
  permissionType?: string | undefined;
  /**
   * The user's prompt text (UserPromptSubmit only)
   */
  prompt?: string | undefined;
  /**
   * The Codex session ID
   */
  sessionId?: string | undefined;
  /**
   * The input to the tool (PreToolUse only)
   */
  toolInput?: any | undefined;
  /**
   * The name of the tool
   */
  toolName?: string | undefined;
  /**
   * The output from the tool (PostToolUse only)
   */
  toolOutput?: any | undefined;
  /**
   * Path to the conversation transcript file
   */
  transcriptPath?: string | undefined;
  /**
   * Email of the authenticated Codex user, if available
   */
  userEmail?: string | undefined;
};
/** @internal */
export declare const CodexHookPayloadHookEventName$outboundSchema: z.ZodMiniEnum<
  typeof CodexHookPayloadHookEventName
>;
/** @internal */
export type CodexHookPayload$Outbound = {
  additional_data?:
    | {
        [k: string]: any;
      }
    | undefined;
  cwd?: string | undefined;
  hook_event_name: string;
  last_assistant_message?: string | undefined;
  model?: string | undefined;
  permission_type?: string | undefined;
  prompt?: string | undefined;
  session_id?: string | undefined;
  tool_input?: any | undefined;
  tool_name?: string | undefined;
  tool_output?: any | undefined;
  transcript_path?: string | undefined;
  user_email?: string | undefined;
};
/** @internal */
export declare const CodexHookPayload$outboundSchema: z.ZodMiniType<
  CodexHookPayload$Outbound,
  CodexHookPayload
>;
export declare function codexHookPayloadToJSON(
  codexHookPayload: CodexHookPayload,
): string;
//# sourceMappingURL=codexhookpayload.d.ts.map
