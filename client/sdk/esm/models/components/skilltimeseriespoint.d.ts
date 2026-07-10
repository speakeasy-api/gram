import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A single time-series bucket for skill usage activity
 */
export type SkillTimeSeriesPoint = {
  /**
   * Bucket start time in Unix nanoseconds (string for JS int64 precision)
   */
  bucketStartNs: string;
  /**
   * Number of skill use events in this bucket
   */
  eventCount: number;
  /**
   * Skill name
   */
  skillName: string;
};
/** @internal */
export declare const SkillTimeSeriesPoint$inboundSchema: z.ZodMiniType<
  SkillTimeSeriesPoint,
  unknown
>;
export declare function skillTimeSeriesPointFromJSON(
  jsonString: string,
): SafeParseResult<SkillTimeSeriesPoint, SDKValidationError>;
//# sourceMappingURL=skilltimeseriespoint.d.ts.map
