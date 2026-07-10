import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ProjectOverviewSummary } from "./projectoverviewsummary.js";
/**
 * Indicates whether metrics are session-based or tool-call-based
 */
export declare const MetricsMode: {
    readonly Session: "session";
    readonly ToolCall: "tool_call";
};
/**
 * Indicates whether metrics are session-based or tool-call-based
 */
export type MetricsMode = ClosedEnum<typeof MetricsMode>;
/**
 * Result of project overview query
 */
export type GetProjectOverviewResult = {
    /**
     * Aggregated project-level summary metrics for a time period
     */
    comparison: ProjectOverviewSummary;
    /**
     * Indicates whether metrics are session-based or tool-call-based
     */
    metricsMode: MetricsMode;
    /**
     * Aggregated project-level summary metrics for a time period
     */
    summary: ProjectOverviewSummary;
};
/** @internal */
export declare const MetricsMode$inboundSchema: z.ZodMiniEnum<typeof MetricsMode>;
/** @internal */
export declare const GetProjectOverviewResult$inboundSchema: z.ZodMiniType<GetProjectOverviewResult, unknown>;
export declare function getProjectOverviewResultFromJSON(jsonString: string): SafeParseResult<GetProjectOverviewResult, SDKValidationError>;
//# sourceMappingURL=getprojectoverviewresult.d.ts.map