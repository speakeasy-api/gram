import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Aggregated hooks metrics for a single user
 */
export type HooksUserSummary = {
  /**
   * Total number of hook events for this user
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
   * Number of successful tool completions (PostToolUse events)
   */
  successCount: number;
  /**
   * Number of unique tools used by this user
   */
  uniqueTools: number;
  /**
   * User email address
   */
  userEmail: string;
};
/** @internal */
export declare const HooksUserSummary$inboundSchema: z.ZodMiniType<
  HooksUserSummary,
  unknown
>;
export declare function hooksUserSummaryFromJSON(
  jsonString: string,
): SafeParseResult<HooksUserSummary, SDKValidationError>;
//# sourceMappingURL=hooksusersummary.d.ts.map
