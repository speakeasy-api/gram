import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Tool usage aggregation target kind
 */
export declare const ToolUsageTargetSummaryTargetKind: {
    readonly Server: "server";
    readonly LocalTools: "local_tools";
    readonly Skill: "skill";
};
/**
 * Tool usage aggregation target kind
 */
export type ToolUsageTargetSummaryTargetKind = ClosedEnum<typeof ToolUsageTargetSummaryTargetKind>;
/**
 * Tool usage target type
 */
export declare const ToolUsageTargetSummaryTargetType: {
    readonly HostedMcpServer: "hosted_mcp_server";
    readonly TunneledMcpServer: "tunneled_mcp_server";
    readonly ShadowMcpServer: "shadow_mcp_server";
    readonly LocalTool: "local_tool";
    readonly Skill: "skill";
};
/**
 * Tool usage target type
 */
export type ToolUsageTargetSummaryTargetType = ClosedEnum<typeof ToolUsageTargetSummaryTargetType>;
/**
 * Aggregated tool usage metrics for one target
 */
export type ToolUsageTargetSummary = {
    /**
     * Total number of tool usage events for the target
     */
    eventCount: number;
    /**
     * Number of failed tool usage events for the target
     */
    failureCount: number;
    /**
     * Fraction of completed tool usage events for the target that failed
     */
    failureRate: number;
    /**
     * Number of successful tool usage events for the target
     */
    successCount: number;
    /**
     * Stable target identifier used by filters and chart grouping
     */
    targetId: string;
    /**
     * Tool usage aggregation target kind
     */
    targetKind: ToolUsageTargetSummaryTargetKind;
    /**
     * User-facing label for the target
     */
    targetLabel: string;
    /**
     * Tool usage target type
     */
    targetType: ToolUsageTargetSummaryTargetType;
    /**
     * Number of distinct tools observed for the target
     */
    uniqueTools: number;
};
/** @internal */
export declare const ToolUsageTargetSummaryTargetKind$inboundSchema: z.ZodMiniEnum<typeof ToolUsageTargetSummaryTargetKind>;
/** @internal */
export declare const ToolUsageTargetSummaryTargetType$inboundSchema: z.ZodMiniEnum<typeof ToolUsageTargetSummaryTargetType>;
/** @internal */
export declare const ToolUsageTargetSummary$inboundSchema: z.ZodMiniType<ToolUsageTargetSummary, unknown>;
export declare function toolUsageTargetSummaryFromJSON(jsonString: string): SafeParseResult<ToolUsageTargetSummary, SDKValidationError>;
//# sourceMappingURL=toolusagetargetsummary.d.ts.map