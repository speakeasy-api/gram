import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Whole-range totals for the billing usage details. Distinct counts (sessions, active users) are computed over the full range and cannot be derived by summing the daily points.
 */
export type TumDetailsTotals = {
    /**
     * Distinct attributed users with usage
     */
    activeUsers: number;
    /**
     * Distinct chat sessions
     */
    agentSessions: number;
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
export declare const TumDetailsTotals$inboundSchema: z.ZodMiniType<TumDetailsTotals, unknown>;
export declare function tumDetailsTotalsFromJSON(jsonString: string): SafeParseResult<TumDetailsTotals, SDKValidationError>;
//# sourceMappingURL=tumdetailstotals.d.ts.map