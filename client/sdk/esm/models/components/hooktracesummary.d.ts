import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Hook execution status
 */
export declare const HookStatus: {
  readonly Success: "success";
  readonly Failure: "failure";
  readonly Pending: "pending";
  readonly Blocked: "blocked";
};
/**
 * Hook execution status
 */
export type HookStatus = ClosedEnum<typeof HookStatus>;
/**
 * Summary information for a hook trace
 */
export type HookTraceSummary = {
  /**
   * Reason set when hook_status is 'blocked' (e.g. shadow-MCP guard rejection)
   */
  blockReason?: string | undefined;
  /**
   * Event source (from materialized column)
   */
  eventSource?: string | undefined;
  /**
   * Gram URN associated with this hook trace
   */
  gramUrn: string;
  /**
   * Hook source (from attributes.gram.hook.source)
   */
  hookSource?: string | undefined;
  /**
   * Hook execution status
   */
  hookStatus?: HookStatus | undefined;
  /**
   * Total number of logs in this trace
   */
  logCount: number;
  /**
   * Skill name (from materialized column, only for Skill tool)
   */
  skillName?: string | undefined;
  /**
   * Earliest log timestamp in Unix nanoseconds (string for JS int64 precision)
   */
  startTimeUnixNano: string;
  /**
   * Tool name (from materialized column)
   */
  toolName?: string | undefined;
  /**
   * Tool call source (from materialized column)
   */
  toolSource?: string | undefined;
  /**
   * Trace ID (32 hex characters)
   */
  traceId: string;
  /**
   * User email (from attributes.user.email)
   */
  userEmail?: string | undefined;
};
/** @internal */
export declare const HookStatus$inboundSchema: z.ZodMiniEnum<typeof HookStatus>;
/** @internal */
export declare const HookTraceSummary$inboundSchema: z.ZodMiniType<
  HookTraceSummary,
  unknown
>;
export declare function hookTraceSummaryFromJSON(
  jsonString: string,
): SafeParseResult<HookTraceSummary, SDKValidationError>;
//# sourceMappingURL=hooktracesummary.d.ts.map
