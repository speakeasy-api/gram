import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * One UTC day of billing usage details
 */
export type TumDetailsPoint = {
    /**
     * Distinct attributed users with usage
     */
    activeUsers: number;
    /**
     * Distinct chat sessions
     */
    agentSessions: number;
    /**
     * Bucket start time in Unix nanoseconds (string for JS precision)
     */
    bucketTimeUnixNano: string;
    /**
     * Cache read input tokens
     */
    cacheReadTokens: number;
    /**
     * Cache creation input tokens
     */
    cacheWriteTokens: number;
    /**
     * Input tokens
     */
    inputTokens: number;
    /**
     * Tokens attributed to MCP tool usage
     */
    mcpToolTokens: number;
    /**
     * Output tokens
     */
    outputTokens: number;
    /**
     * Tokens in messages carrying at least one active risk finding
     */
    riskyMessageTokens: number;
    /**
     * Tokens attributed to skill usage
     */
    skillTokens: number;
    /**
     * Completed tool calls
     */
    toolCalls: number;
    /**
     * Tokens in tool-call messages
     */
    toolMessageTokens: number;
    /**
     * All tokens
     */
    totalTokens: number;
    /**
     * Tokens without user attribution
     */
    unattributedTokens: number;
};
/** @internal */
export declare const TumDetailsPoint$inboundSchema: z.ZodMiniType<TumDetailsPoint, unknown>;
export declare function tumDetailsPointFromJSON(jsonString: string): SafeParseResult<TumDetailsPoint, SDKValidationError>;
//# sourceMappingURL=tumdetailspoint.d.ts.map