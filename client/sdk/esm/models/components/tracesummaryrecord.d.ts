import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Summary information for a distributed trace
 */
export type TraceSummaryRecord = {
  /**
   * Gram URN associated with this trace
   */
  gramUrn: string;
  /**
   * HTTP status code (if applicable)
   */
  httpStatusCode?: number | undefined;
  /**
   * Total number of logs in this trace
   */
  logCount: number;
  /**
   * Earliest log timestamp in Unix nanoseconds
   */
  startTimeUnixNano: number;
  /**
   * Trace ID (32 hex characters)
   */
  traceId: string;
};
/** @internal */
export declare const TraceSummaryRecord$inboundSchema: z.ZodMiniType<
  TraceSummaryRecord,
  unknown
>;
export declare function traceSummaryRecordFromJSON(
  jsonString: string,
): SafeParseResult<TraceSummaryRecord, SDKValidationError>;
//# sourceMappingURL=tracesummaryrecord.d.ts.map
