import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A single time-series bucket for hooks activity
 */
export type HooksTimeSeriesPoint = {
  /**
   * Bucket start time in Unix nanoseconds (string for JS int64 precision)
   */
  bucketStartNs: string;
  /**
   * Number of events in this bucket
   */
  eventCount: number;
  /**
   * Number of failed hook events in this bucket
   */
  failureCount: number;
  /**
   * Server name
   */
  serverName: string;
  /**
   * User email address
   */
  userEmail: string;
};
/** @internal */
export declare const HooksTimeSeriesPoint$inboundSchema: z.ZodMiniType<
  HooksTimeSeriesPoint,
  unknown
>;
export declare function hooksTimeSeriesPointFromJSON(
  jsonString: string,
): SafeParseResult<HooksTimeSeriesPoint, SDKValidationError>;
//# sourceMappingURL=hookstimeseriespoint.d.ts.map
