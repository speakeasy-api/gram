import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Aggregated hooks metrics for a single server
 */
export type HooksServerSummary = {
    /**
     * Total number of hook events for this server
     */
    eventCount: number;
    /**
     * Number of failed tool completions (PostToolUseFailure events)
     */
    failureCount: number;
    /**
     * Failure rate as a decimal (0.0 to 1.0)
     */
    failureRate: number;
    /**
     * Server name (extracted from tool name, or 'local' for non-MCP tools)
     */
    serverName: string;
    /**
     * Number of successful tool completions (PostToolUse events)
     */
    successCount: number;
    /**
     * Number of unique tools used for this server
     */
    uniqueTools: number;
};
/** @internal */
export declare const HooksServerSummary$inboundSchema: z.ZodMiniType<HooksServerSummary, unknown>;
export declare function hooksServerSummaryFromJSON(jsonString: string): SafeParseResult<HooksServerSummary, SDKValidationError>;
//# sourceMappingURL=hooksserversummary.d.ts.map