import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Target-aware MCP and tool usage totals
 */
export type ToolUsageTotals = {
    /**
     * Total number of tool usage events
     */
    eventCount: number;
    /**
     * Number of failed tool usage events
     */
    failureCount: number;
    /**
     * Fraction of completed tool usage events that failed
     */
    failureRate: number;
    /**
     * Number of successful tool usage events
     */
    successCount: number;
    /**
     * Number of distinct usage targets observed
     */
    uniqueTargets: number;
    /**
     * Number of distinct tools observed
     */
    uniqueTools: number;
    /**
     * Number of distinct user identities observed
     */
    uniqueUsers: number;
};
/** @internal */
export declare const ToolUsageTotals$inboundSchema: z.ZodMiniType<ToolUsageTotals, unknown>;
export declare function toolUsageTotalsFromJSON(jsonString: string): SafeParseResult<ToolUsageTotals, SDKValidationError>;
//# sourceMappingURL=toolusagetotals.d.ts.map