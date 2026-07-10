import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ClaudeTurnUsage = {
    /**
     * Cache creation tokens used by this turn.
     */
    cacheCreationTokens: number;
    /**
     * Cache read tokens used by this turn.
     */
    cacheReadTokens: number;
    /**
     * Total cost for this turn in micros of a USD.
     */
    costMicros: number;
    /**
     * Total USD cost for this turn.
     */
    costUsd: number;
    /**
     * Latest OTEL log timestamp in this turn, as Unix nanoseconds.
     */
    endTimeUnixNano: string;
    /**
     * Input tokens used by this turn.
     */
    inputTokens: number;
    /**
     * Distinct model names used by this turn.
     */
    models: Array<string>;
    /**
     * Output tokens used by this turn.
     */
    outputTokens: number;
    /**
     * Claude prompt.id that correlates events for one user turn.
     */
    promptId: string;
    /**
     * Distinct Claude query sources used by this turn.
     */
    querySources: Array<string>;
    /**
     * Number of Claude API request events in this turn.
     */
    requestCount: number;
    /**
     * Earliest OTEL log timestamp in this turn, as Unix nanoseconds.
     */
    startTimeUnixNano: string;
    /**
     * Total tokens used by this turn.
     */
    totalTokens: number;
};
/** @internal */
export declare const ClaudeTurnUsage$inboundSchema: z.ZodMiniType<ClaudeTurnUsage, unknown>;
export declare function claudeTurnUsageFromJSON(jsonString: string): SafeParseResult<ClaudeTurnUsage, SDKValidationError>;
//# sourceMappingURL=claudeturnusage.d.ts.map