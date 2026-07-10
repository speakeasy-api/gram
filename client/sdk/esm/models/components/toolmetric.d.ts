import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Aggregated metrics for a single tool
 */
export type ToolMetric = {
    /**
     * Average latency in milliseconds
     */
    avgLatencyMs: number;
    /**
     * Total number of calls
     */
    callCount: number;
    /**
     * Number of failed calls
     */
    failureCount: number;
    /**
     * Failure rate (0.0 to 1.0)
     */
    failureRate: number;
    /**
     * Tool URN
     */
    gramUrn: string;
    /**
     * Number of successful calls
     */
    successCount: number;
};
/** @internal */
export declare const ToolMetric$inboundSchema: z.ZodMiniType<ToolMetric, unknown>;
export declare function toolMetricFromJSON(jsonString: string): SafeParseResult<ToolMetric, SDKValidationError>;
//# sourceMappingURL=toolmetric.d.ts.map