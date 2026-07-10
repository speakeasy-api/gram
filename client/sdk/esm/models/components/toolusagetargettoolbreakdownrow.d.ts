import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Tool usage aggregation target kind
 */
export declare const ToolUsageTargetToolBreakdownRowTargetKind: {
    readonly Server: "server";
    readonly LocalTools: "local_tools";
    readonly Skill: "skill";
};
/**
 * Tool usage aggregation target kind
 */
export type ToolUsageTargetToolBreakdownRowTargetKind = ClosedEnum<typeof ToolUsageTargetToolBreakdownRowTargetKind>;
/**
 * Tool usage target type
 */
export declare const ToolUsageTargetToolBreakdownRowTargetType: {
    readonly HostedMcpServer: "hosted_mcp_server";
    readonly TunneledMcpServer: "tunneled_mcp_server";
    readonly ShadowMcpServer: "shadow_mcp_server";
    readonly LocalTool: "local_tool";
    readonly Skill: "skill";
};
/**
 * Tool usage target type
 */
export type ToolUsageTargetToolBreakdownRowTargetType = ClosedEnum<typeof ToolUsageTargetToolBreakdownRowTargetType>;
/**
 * Aggregated tool usage metrics for one target and tool
 */
export type ToolUsageTargetToolBreakdownRow = {
    /**
     * Total number of tool usage events for the target and tool
     */
    eventCount: number;
    /**
     * Number of failed tool usage events for the target and tool
     */
    failureCount: number;
    /**
     * Fraction of completed tool usage events for the target and tool that failed
     */
    failureRate: number;
    /**
     * Number of successful tool usage events for the target and tool
     */
    successCount: number;
    /**
     * Stable target identifier used by filters and chart grouping
     */
    targetId: string;
    /**
     * Tool usage aggregation target kind
     */
    targetKind: ToolUsageTargetToolBreakdownRowTargetKind;
    /**
     * User-facing label for the target
     */
    targetLabel: string;
    /**
     * Tool usage target type
     */
    targetType: ToolUsageTargetToolBreakdownRowTargetType;
    /**
     * Observed tool name
     */
    toolName: string;
};
/** @internal */
export declare const ToolUsageTargetToolBreakdownRowTargetKind$inboundSchema: z.ZodMiniEnum<typeof ToolUsageTargetToolBreakdownRowTargetKind>;
/** @internal */
export declare const ToolUsageTargetToolBreakdownRowTargetType$inboundSchema: z.ZodMiniEnum<typeof ToolUsageTargetToolBreakdownRowTargetType>;
/** @internal */
export declare const ToolUsageTargetToolBreakdownRow$inboundSchema: z.ZodMiniType<ToolUsageTargetToolBreakdownRow, unknown>;
export declare function toolUsageTargetToolBreakdownRowFromJSON(jsonString: string): SafeParseResult<ToolUsageTargetToolBreakdownRow, SDKValidationError>;
//# sourceMappingURL=toolusagetargettoolbreakdownrow.d.ts.map