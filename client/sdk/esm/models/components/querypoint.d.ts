import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { QueryMeasures } from "./querymeasures.js";
/**
 * A single time bucket within a series
 */
export type QueryPoint = {
  /**
   * Bucket start time in Unix nanoseconds (string for JS precision)
   */
  bucketTimeUnixNano: string;
  /**
   * Aggregated measure values for a group or time bucket
   */
  measures: QueryMeasures;
};
/** @internal */
export declare const QueryPoint$inboundSchema: z.ZodMiniType<
  QueryPoint,
  unknown
>;
export declare function queryPointFromJSON(
  jsonString: string,
): SafeParseResult<QueryPoint, SDKValidationError>;
//# sourceMappingURL=querypoint.d.ts.map
