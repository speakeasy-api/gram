import * as z from "zod/v4-mini";
import { OTELSum, OTELSum$Outbound } from "./otelsum.js";
/**
 * OTEL metric
 */
export type OTELMetric = {
  /**
   * Metric description
   */
  description?: string | undefined;
  /**
   * ExponentialHistogram metric data (passed through)
   */
  exponentialHistogram?: any | undefined;
  /**
   * Gauge metric data (passed through)
   */
  gauge?: any | undefined;
  /**
   * Histogram metric data (passed through)
   */
  histogram?: any | undefined;
  /**
   * Metric name
   */
  name?: string | undefined;
  /**
   * OTEL sum metric
   */
  sum?: OTELSum | undefined;
  /**
   * Summary metric data (passed through)
   */
  summary?: any | undefined;
  /**
   * Metric unit
   */
  unit?: string | undefined;
};
/** @internal */
export type OTELMetric$Outbound = {
  description?: string | undefined;
  exponentialHistogram?: any | undefined;
  gauge?: any | undefined;
  histogram?: any | undefined;
  name?: string | undefined;
  sum?: OTELSum$Outbound | undefined;
  summary?: any | undefined;
  unit?: string | undefined;
};
/** @internal */
export declare const OTELMetric$outboundSchema: z.ZodMiniType<
  OTELMetric$Outbound,
  OTELMetric
>;
export declare function otelMetricToJSON(otelMetric: OTELMetric): string;
//# sourceMappingURL=otelmetric.d.ts.map
