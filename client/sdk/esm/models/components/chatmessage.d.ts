import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ChatMessage = {
  /**
   * The content of the message — string for plain text, array for multimodal/tool-call content parts, null for assistant messages that only carry tool_calls
   */
  content?: any | undefined;
  /**
   * When the message was created.
   */
  createdAt: Date;
  /**
   * The ID of the external user who created the message
   */
  externalUserId?: string | undefined;
  /**
   * The finish reason of the message
   */
  finishReason?: string | undefined;
  /**
   * Conversation generation — bumps on compaction or edit divergence
   */
  generation: number;
  /**
   * The ID of the message
   */
  id: string;
  /**
   * Present only in `risk_only` mode: true when this message has an active risk finding, false for the surrounding-context messages padded around it.
   */
  isRisk?: boolean | undefined;
  /**
   * The model that generated the message
   */
  model: string;
  /**
   * The agent prompt/turn ID associated with this message, when available.
   */
  promptId?: string | undefined;
  /**
   * The role of the message
   */
  role: string;
  /**
   * Monotonic sequence number of the message. Strictly increasing within a chat; use it as the keyset cursor for `before_seq`/`after_seq` pagination. Not contiguous (the sequence is shared across chats), so do not infer gaps from arithmetic differences.
   */
  seq: number;
  /**
   * The tool call ID of the message
   */
  toolCallId?: string | undefined;
  /**
   * The tool calls in the message as a JSON blob
   */
  toolCalls?: string | undefined;
  /**
   * The ID of the user who created the message
   */
  userId?: string | undefined;
};
/** @internal */
export declare const ChatMessage$inboundSchema: z.ZodMiniType<
  ChatMessage,
  unknown
>;
export declare function chatMessageFromJSON(
  jsonString: string,
): SafeParseResult<ChatMessage, SDKValidationError>;
//# sourceMappingURL=chatmessage.d.ts.map
