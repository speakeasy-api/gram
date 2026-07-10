import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Aggregated summary metrics for a time period
 */
export type ObservabilitySummary = {
    /**
     * Average tool latency in milliseconds
     */
    avgLatencyMs: number;
    /**
     * Average time to resolution in milliseconds
     */
    avgResolutionTimeMs: number;
    /**
     * Average session duration in milliseconds
     */
    avgSessionDurationMs: number;
    /**
     * Sum of cache creation input tokens
     */
    cacheCreationInputTokens: number;
    /**
     * Sum of cache read input tokens
     */
    cacheReadInputTokens: number;
    /**
     * Number of failed chat sessions
     */
    failedChats: number;
    /**
     * Number of failed tool calls
     */
    failedToolCalls: number;
    /**
     * Number of resolved chat sessions
     */
    resolvedChats: number;
    /**
     * Total number of chat sessions
     */
    totalChats: number;
    /**
     * Total cost of all requests
     */
    totalCost: number;
    /**
     * Sum of input tokens used
     */
    totalInputTokens: number;
    /**
     * Sum of output tokens used
     */
    totalOutputTokens: number;
    /**
     * Sum of all tokens used
     */
    totalTokens: number;
    /**
     * Total number of tool calls
     */
    totalToolCalls: number;
};
/** @internal */
export declare const ObservabilitySummary$inboundSchema: z.ZodMiniType<ObservabilitySummary, unknown>;
export declare function observabilitySummaryFromJSON(jsonString: string): SafeParseResult<ObservabilitySummary, SDKValidationError>;
//# sourceMappingURL=observabilitysummary.d.ts.map