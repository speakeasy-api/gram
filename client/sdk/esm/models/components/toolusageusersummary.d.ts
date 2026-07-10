import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Tool usage user identity kind
 */
export declare const ToolUsageUserSummaryUserKind: {
  readonly Email: "email";
  readonly ExternalUserId: "external_user_id";
  readonly UserId: "user_id";
  readonly Unknown: "unknown";
};
/**
 * Tool usage user identity kind
 */
export type ToolUsageUserSummaryUserKind = ClosedEnum<
  typeof ToolUsageUserSummaryUserKind
>;
/**
 * Aggregated tool usage metrics for one user identity
 */
export type ToolUsageUserSummary = {
  /**
   * Total number of tool usage events for the user identity
   */
  eventCount: number;
  /**
   * Number of failed tool usage events for the user identity
   */
  failureCount: number;
  /**
   * Fraction of completed tool usage events for the user identity that failed
   */
  failureRate: number;
  /**
   * Number of successful tool usage events for the user identity
   */
  successCount: number;
  /**
   * Number of distinct tools observed for the user identity
   */
  uniqueTools: number;
  /**
   * Stable user identity value used by filters and chart grouping
   */
  userKey: string;
  /**
   * Tool usage user identity kind
   */
  userKind: ToolUsageUserSummaryUserKind;
  /**
   * User-facing label for the user identity
   */
  userLabel: string;
};
/** @internal */
export declare const ToolUsageUserSummaryUserKind$inboundSchema: z.ZodMiniEnum<
  typeof ToolUsageUserSummaryUserKind
>;
/** @internal */
export declare const ToolUsageUserSummary$inboundSchema: z.ZodMiniType<
  ToolUsageUserSummary,
  unknown
>;
export declare function toolUsageUserSummaryFromJSON(
  jsonString: string,
): SafeParseResult<ToolUsageUserSummary, SDKValidationError>;
//# sourceMappingURL=toolusageusersummary.d.ts.map
