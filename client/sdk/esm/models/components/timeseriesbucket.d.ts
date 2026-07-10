import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A single time bucket for time series metrics
 */
export type TimeSeriesBucket = {
    /**
     * Abandoned chat sessions in this bucket
     */
    abandonedChats: number;
    /**
     * Average session duration in milliseconds
     */
    avgSessionDurationMs: number;
    /**
     * Average tool latency in milliseconds
     */
    avgToolLatencyMs: number;
    /**
     * Bucket start time in Unix nanoseconds (string for JS precision)
     */
    bucketTimeUnixNano: string;
    /**
     * Sum of cache creation input tokens in this bucket
     */
    cacheCreationInputTokens: number;
    /**
     * Sum of cache read input tokens in this bucket
     */
    cacheReadInputTokens: number;
    /**
     * Failed chat sessions in this bucket
     */
    failedChats: number;
    /**
     * Failed tool calls in this bucket
     */
    failedToolCalls: number;
    /**
     * Partially resolved chat sessions in this bucket
     */
    partialChats: number;
    /**
     * Resolved chat sessions in this bucket
     */
    resolvedChats: number;
    /**
     * Total chat sessions in this bucket
     */
    totalChats: number;
    /**
     * Total cost in this bucket
     */
    totalCost: number;
    /**
     * Sum of input tokens in this bucket
     */
    totalInputTokens: number;
    /**
     * Sum of output tokens in this bucket
     */
    totalOutputTokens: number;
    /**
     * Sum of all tokens in this bucket
     */
    totalTokens: number;
    /**
     * Total tool calls in this bucket
     */
    totalToolCalls: number;
};
/** @internal */
export declare const TimeSeriesBucket$inboundSchema: z.ZodMiniType<TimeSeriesBucket, unknown>;
export declare function timeSeriesBucketFromJSON(jsonString: string): SafeParseResult<TimeSeriesBucket, SDKValidationError>;
//# sourceMappingURL=timeseriesbucket.d.ts.map