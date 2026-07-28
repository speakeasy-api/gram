import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ObservabilitySummary } from "./observabilitysummary.js";
import { TimeSeriesBucket } from "./timeseriesbucket.js";
import { ToolMetric } from "./toolmetric.js";
/**
 * Result of observability overview query
 */
export type GetObservabilityOverviewResult = {
  /**
   * Aggregated summary metrics for a time period
   */
  comparison: ObservabilitySummary;
  /**
   * The time bucket interval in seconds used for the time series data
   */
  intervalSeconds: number;
  /**
   * Aggregated summary metrics for a time period
   */
  summary: ObservabilitySummary;
  /**
   * Time series data points
   */
  timeSeries: Array<TimeSeriesBucket>;
  /**
   * Top tools by call count
   */
  topToolsByCount: Array<ToolMetric>;
  /**
   * Top tools by failure rate
   */
  topToolsByFailureRate: Array<ToolMetric>;
};
/** @internal */
export declare const GetObservabilityOverviewResult$inboundSchema: z.ZodMiniType<
  GetObservabilityOverviewResult,
  unknown
>;
export declare function getObservabilityOverviewResultFromJSON(
  jsonString: string,
): SafeParseResult<GetObservabilityOverviewResult, SDKValidationError>;
//# sourceMappingURL=getobservabilityoverviewresult.d.ts.map
