import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Chat session status
 */
export declare const ChatSummaryStatus: {
    readonly Success: "success";
    readonly Error: "error";
};
/**
 * Chat session status
 */
export type ChatSummaryStatus = ClosedEnum<typeof ChatSummaryStatus>;
/**
 * Summary information for a chat session
 */
export type ChatSummary = {
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
     * Total number of logs in this chat session
     */
    logCount: number;
    /**
     * Number of LLM completion messages in this chat session
     */
    messageCount: number;
    /**
     * LLM model used in this chat session
     */
    model?: string | undefined;
    /**
     * Earliest log timestamp in Unix nanoseconds (string for JS int64 precision)
     */
    startTimeUnixNano: string;
    /**
     * Chat session status
     */
    status: ChatSummaryStatus;
    /**
     * Number of tool calls in this chat session
     */
    toolCallCount: number;
    /**
     * Total input tokens used
     */
    totalInputTokens: number;
    /**
     * Total output tokens used
     */
    totalOutputTokens: number;
    /**
     * Total tokens used (input + output)
     */
    totalTokens: number;
    /**
     * User ID associated with this chat session
     */
    userId?: string | undefined;
};
/** @internal */
export declare const ChatSummaryStatus$inboundSchema: z.ZodMiniEnum<typeof ChatSummaryStatus>;
/** @internal */
export declare const ChatSummary$inboundSchema: z.ZodMiniType<ChatSummary, unknown>;
export declare function chatSummaryFromJSON(jsonString: string): SafeParseResult<ChatSummary, SDKValidationError>;
//# sourceMappingURL=chatsummary.d.ts.map