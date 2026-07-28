import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ProjectSummary } from "./projectsummary.js";
/**
 * Result of metrics summary query
 */
export type GetMetricsSummaryResult = {
  /**
   * Aggregated metrics
   */
  metrics: ProjectSummary;
};
/** @internal */
export declare const GetMetricsSummaryResult$inboundSchema: z.ZodMiniType<
  GetMetricsSummaryResult,
  unknown
>;
export declare function getMetricsSummaryResultFromJSON(
  jsonString: string,
): SafeParseResult<GetMetricsSummaryResult, SDKValidationError>;
//# sourceMappingURL=getmetricssummaryresult.d.ts.map
