import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ToolUsageTraceLogGroup } from "./toolusagetraceloggroup.js";
/**
 * Hook execution status when the row came from hook telemetry
 */
export declare const ToolUsageTraceSummaryHookStatus: {
    readonly Success: "success";
    readonly Failure: "failure";
    readonly Blocked: "blocked";
    readonly Pending: "pending";
};
/**
 * Hook execution status when the row came from hook telemetry
 */
export type ToolUsageTraceSummaryHookStatus = ClosedEnum<typeof ToolUsageTraceSummaryHookStatus>;
/**
 * Tool usage aggregation target kind
 */
export declare const ToolUsageTraceSummaryTargetKind: {
    readonly Server: "server";
    readonly LocalTools: "local_tools";
    readonly Skill: "skill";
};
/**
 * Tool usage aggregation target kind
 */
export type ToolUsageTraceSummaryTargetKind = ClosedEnum<typeof ToolUsageTraceSummaryTargetKind>;
/**
 * Tool usage target type
 */
export declare const ToolUsageTraceSummaryTargetType: {
    readonly HostedMcpServer: "hosted_mcp_server";
    readonly TunneledMcpServer: "tunneled_mcp_server";
    readonly ShadowMcpServer: "shadow_mcp_server";
    readonly LocalTool: "local_tool";
    readonly Skill: "skill";
};
/**
 * Tool usage target type
 */
export type ToolUsageTraceSummaryTargetType = ClosedEnum<typeof ToolUsageTraceSummaryTargetType>;
/**
 * Tool usage user identity kind
 */
export declare const ToolUsageTraceSummaryUserKind: {
    readonly Email: "email";
    readonly ExternalUserId: "external_user_id";
    readonly UserId: "user_id";
    readonly Unknown: "unknown";
};
/**
 * Tool usage user identity kind
 */
export type ToolUsageTraceSummaryUserKind = ClosedEnum<typeof ToolUsageTraceSummaryUserKind>;
/**
 * A single target-aware tool usage trace row
 */
export type ToolUsageTraceSummary = {
    /**
     * AI account classification ('team' or 'personal'); empty/absent when unclassified
     */
    accountType?: string | undefined;
    /**
     * Hook block reason when hook_status is blocked
     */
    blockReason?: string | undefined;
    /**
     * Telemetry event source
     */
    eventSource: string;
    /**
     * Gram URN associated with the trace
     */
    gramUrn: string;
    /**
     * Hook plugin source when the row came from hook telemetry
     */
    hookSource?: string | undefined;
    /**
     * Hook execution status when the row came from hook telemetry
     */
    hookStatus?: ToolUsageTraceSummaryHookStatus | undefined;
    /**
     * HTTP status code when available
     */
    httpStatusCode?: number | undefined;
    /**
     * Stable row identity for React keys and expansion state
     */
    id: string;
    /**
     * Number of logs in the trace
     */
    logCount: number;
    /**
     * Descriptor used by the dashboard to fetch child logs for a trace row
     */
    logGroup: ToolUsageTraceLogGroup;
    /**
     * Earliest log timestamp in Unix nanoseconds as a string for JavaScript integer safety
     */
    startTimeUnixNano: string;
    /**
     * Stable target identifier used by filters
     */
    targetId: string;
    /**
     * Tool usage aggregation target kind
     */
    targetKind: ToolUsageTraceSummaryTargetKind;
    /**
     * User-facing target label
     */
    targetLabel: string;
    /**
     * Tool usage target type
     */
    targetType: ToolUsageTraceSummaryTargetType;
    /**
     * Tool name shown in the row
     */
    toolName: string;
    /**
     * Real OTel trace ID when the grouped logs have one
     */
    traceId?: string | undefined;
    /**
     * Stable user identity value
     */
    userKey: string;
    /**
     * Tool usage user identity kind
     */
    userKind: ToolUsageTraceSummaryUserKind;
    /**
     * User-facing user identity label
     */
    userLabel: string;
};
/** @internal */
export declare const ToolUsageTraceSummaryHookStatus$inboundSchema: z.ZodMiniEnum<typeof ToolUsageTraceSummaryHookStatus>;
/** @internal */
export declare const ToolUsageTraceSummaryTargetKind$inboundSchema: z.ZodMiniEnum<typeof ToolUsageTraceSummaryTargetKind>;
/** @internal */
export declare const ToolUsageTraceSummaryTargetType$inboundSchema: z.ZodMiniEnum<typeof ToolUsageTraceSummaryTargetType>;
/** @internal */
export declare const ToolUsageTraceSummaryUserKind$inboundSchema: z.ZodMiniEnum<typeof ToolUsageTraceSummaryUserKind>;
/** @internal */
export declare const ToolUsageTraceSummary$inboundSchema: z.ZodMiniType<ToolUsageTraceSummary, unknown>;
export declare function toolUsageTraceSummaryFromJSON(jsonString: string): SafeParseResult<ToolUsageTraceSummary, SDKValidationError>;
//# sourceMappingURL=toolusagetracesummary.d.ts.map