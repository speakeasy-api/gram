import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Chat session status
 */
export declare const SessionSummaryStatus: {
  readonly Success: "success";
  readonly Error: "error";
};
/**
 * Chat session status
 */
export type SessionSummaryStatus = ClosedEnum<typeof SessionSummaryStatus>;
/**
 * Org-scoped summary information for a chat session
 */
export type SessionSummary = {
  /**
   * Chat session duration in seconds
   */
  durationSeconds: number;
  /**
   * Latest log timestamp in Unix nanoseconds (string for JS int64 precision)
   */
  endTimeUnixNano: string;
  /**
   * Chat session ID
   */
  gramChatId: string;
  /**
   * Client or agent surface associated with this chat session
   */
  hookSource?: string | undefined;
  /**
   * Number of LLM completion messages in this chat session
   */
  messageCount: number;
  /**
   * LLM model used in this chat session
   */
  model?: string | undefined;
  /**
   * Project ID that emitted this chat session
   */
  projectId: string;
  /**
   * Earliest log timestamp in Unix nanoseconds (string for JS int64 precision)
   */
  startTimeUnixNano: string;
  /**
   * Chat session status
   */
  status: SessionSummaryStatus;
  /**
   * Chat title, when the session resolves to a named chat
   */
  title?: string | undefined;
  /**
   * Number of tool calls in this chat session
   */
  toolCallCount: number;
  /**
   * Total cost in USD
   */
  totalCost: number;
  /**
   * Total input tokens used
   */
  totalInputTokens: number;
  /**
   * Total output tokens used
   */
  totalOutputTokens: number;
  /**
   * Total tokens used
   */
  totalTokens: number;
  /**
   * User email associated with this chat session
   */
  userEmail?: string | undefined;
};
/** @internal */
export declare const SessionSummaryStatus$inboundSchema: z.ZodMiniEnum<
  typeof SessionSummaryStatus
>;
/** @internal */
export declare const SessionSummary$inboundSchema: z.ZodMiniType<
  SessionSummary,
  unknown
>;
export declare function sessionSummaryFromJSON(
  jsonString: string,
): SafeParseResult<SessionSummary, SDKValidationError>;
//# sourceMappingURL=sessionsummary.d.ts.map
