import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Tool usage aggregation target kind
 */
export declare const TargetKind: {
  readonly Server: "server";
  readonly LocalTools: "local_tools";
  readonly Skill: "skill";
};
/**
 * Tool usage aggregation target kind
 */
export type TargetKind = ClosedEnum<typeof TargetKind>;
/**
 * Tool usage target type
 */
export declare const TargetType: {
  readonly HostedMcpServer: "hosted_mcp_server";
  readonly TunneledMcpServer: "tunneled_mcp_server";
  readonly ShadowMcpServer: "shadow_mcp_server";
  readonly LocalTool: "local_tool";
  readonly Skill: "skill";
};
/**
 * Tool usage target type
 */
export type TargetType = ClosedEnum<typeof TargetType>;
/**
 * A time-series bucket for one tool usage target
 */
export type ToolUsageTargetTimeSeriesPoint = {
  /**
   * Bucket start time in Unix nanoseconds as a string for JavaScript integer safety
   */
  bucketStartNs: string;
  /**
   * Number of tool usage events in the bucket
   */
  eventCount: number;
  /**
   * Number of failed tool usage events in the bucket
   */
  failureCount: number;
  /**
   * Stable target identifier used by filters and chart grouping
   */
  targetId: string;
  /**
   * Tool usage aggregation target kind
   */
  targetKind: TargetKind;
  /**
   * User-facing label for the target
   */
  targetLabel: string;
  /**
   * Tool usage target type
   */
  targetType: TargetType;
};
/** @internal */
export declare const TargetKind$inboundSchema: z.ZodMiniEnum<typeof TargetKind>;
/** @internal */
export declare const TargetType$inboundSchema: z.ZodMiniEnum<typeof TargetType>;
/** @internal */
export declare const ToolUsageTargetTimeSeriesPoint$inboundSchema: z.ZodMiniType<
  ToolUsageTargetTimeSeriesPoint,
  unknown
>;
export declare function toolUsageTargetTimeSeriesPointFromJSON(
  jsonString: string,
): SafeParseResult<ToolUsageTargetTimeSeriesPoint, SDKValidationError>;
//# sourceMappingURL=toolusagetargettimeseriespoint.d.ts.map
