import * as z from "zod/v4-mini";
import {
  OTELNumberDataPoint,
  OTELNumberDataPoint$Outbound,
} from "./otelnumberdatapoint.js";
/**
 * OTEL sum metric
 */
export type OTELSum = {
  /**
   * Aggregation temporality (number or enum string)
   */
  aggregationTemporality?: any | undefined;
  /**
   * Data points
   */
  dataPoints?: Array<OTELNumberDataPoint> | undefined;
  /**
   * Whether the sum is monotonic
   */
  isMonotonic?: boolean | undefined;
};
/** @internal */
export type OTELSum$Outbound = {
  aggregationTemporality?: any | undefined;
  dataPoints?: Array<OTELNumberDataPoint$Outbound> | undefined;
  isMonotonic?: boolean | undefined;
};
/** @internal */
export declare const OTELSum$outboundSchema: z.ZodMiniType<
  OTELSum$Outbound,
  OTELSum
>;
export declare function otelSumToJSON(otelSum: OTELSum): string;
//# sourceMappingURL=otelsum.d.ts.map
