import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ProjectSummary } from "./projectsummary.js";
/**
 * Result of user metrics summary query
 */
export type GetUserMetricsSummaryResult = {
  /**
   * Aggregated metrics
   */
  metrics: ProjectSummary;
};
/** @internal */
export declare const GetUserMetricsSummaryResult$inboundSchema: z.ZodMiniType<
  GetUserMetricsSummaryResult,
  unknown
>;
export declare function getUserMetricsSummaryResultFromJSON(
  jsonString: string,
): SafeParseResult<GetUserMetricsSummaryResult, SDKValidationError>;
//# sourceMappingURL=getusermetricssummaryresult.d.ts.map
