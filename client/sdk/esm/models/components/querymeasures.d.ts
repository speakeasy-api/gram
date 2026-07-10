import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Aggregated measure values for a group or time bucket
 */
export type QueryMeasures = {
    /**
     * Sum of cache creation input tokens
     */
    cacheCreationInputTokens: number;
    /**
     * Sum of cache read input tokens
     */
    cacheReadInputTokens: number;
    /**
     * Number of distinct chat sessions
     */
    totalChats: number;
    /**
     * Total cost in USD
     */
    totalCost: number;
    /**
     * Sum of input tokens
     */
    totalInputTokens: number;
    /**
     * Sum of output tokens
     */
    totalOutputTokens: number;
    /**
     * Sum of all tokens
     */
    totalTokens: number;
    /**
     * Total number of tool calls
     */
    totalToolCalls: number;
};
/** @internal */
export declare const QueryMeasures$inboundSchema: z.ZodMiniType<QueryMeasures, unknown>;
export declare function queryMeasuresFromJSON(jsonString: string): SafeParseResult<QueryMeasures, SDKValidationError>;
//# sourceMappingURL=querymeasures.d.ts.map