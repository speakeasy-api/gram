import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Tool usage aggregation target kind
 */
export declare const ToolUsageUsersByTargetRowTargetKind: {
  readonly Server: "server";
  readonly LocalTools: "local_tools";
  readonly Skill: "skill";
};
/**
 * Tool usage aggregation target kind
 */
export type ToolUsageUsersByTargetRowTargetKind = ClosedEnum<
  typeof ToolUsageUsersByTargetRowTargetKind
>;
/**
 * Tool usage target type
 */
export declare const ToolUsageUsersByTargetRowTargetType: {
  readonly HostedMcpServer: "hosted_mcp_server";
  readonly TunneledMcpServer: "tunneled_mcp_server";
  readonly ShadowMcpServer: "shadow_mcp_server";
  readonly LocalTool: "local_tool";
  readonly Skill: "skill";
};
/**
 * Tool usage target type
 */
export type ToolUsageUsersByTargetRowTargetType = ClosedEnum<
  typeof ToolUsageUsersByTargetRowTargetType
>;
/**
 * Tool usage user identity kind
 */
export declare const ToolUsageUsersByTargetRowUserKind: {
  readonly Email: "email";
  readonly ExternalUserId: "external_user_id";
  readonly UserId: "user_id";
  readonly Unknown: "unknown";
};
/**
 * Tool usage user identity kind
 */
export type ToolUsageUsersByTargetRowUserKind = ClosedEnum<
  typeof ToolUsageUsersByTargetRowUserKind
>;
/**
 * Aggregated tool usage metrics for one target and user identity
 */
export type ToolUsageUsersByTargetRow = {
  /**
   * Total number of tool usage events for the target and user identity
   */
  eventCount: number;
  /**
   * Number of failed tool usage events for the target and user identity
   */
  failureCount: number;
  /**
   * Stable target identifier used by filters and chart grouping
   */
  targetId: string;
  /**
   * Tool usage aggregation target kind
   */
  targetKind: ToolUsageUsersByTargetRowTargetKind;
  /**
   * User-facing label for the target
   */
  targetLabel: string;
  /**
   * Tool usage target type
   */
  targetType: ToolUsageUsersByTargetRowTargetType;
  /**
   * Stable user identity value used by filters and chart grouping
   */
  userKey: string;
  /**
   * Tool usage user identity kind
   */
  userKind: ToolUsageUsersByTargetRowUserKind;
  /**
   * User-facing label for the user identity
   */
  userLabel: string;
};
/** @internal */
export declare const ToolUsageUsersByTargetRowTargetKind$inboundSchema: z.ZodMiniEnum<
  typeof ToolUsageUsersByTargetRowTargetKind
>;
/** @internal */
export declare const ToolUsageUsersByTargetRowTargetType$inboundSchema: z.ZodMiniEnum<
  typeof ToolUsageUsersByTargetRowTargetType
>;
/** @internal */
export declare const ToolUsageUsersByTargetRowUserKind$inboundSchema: z.ZodMiniEnum<
  typeof ToolUsageUsersByTargetRowUserKind
>;
/** @internal */
export declare const ToolUsageUsersByTargetRow$inboundSchema: z.ZodMiniType<
  ToolUsageUsersByTargetRow,
  unknown
>;
export declare function toolUsageUsersByTargetRowFromJSON(
  jsonString: string,
): SafeParseResult<ToolUsageUsersByTargetRow, SDKValidationError>;
//# sourceMappingURL=toolusageusersbytargetrow.d.ts.map
