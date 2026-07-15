import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type RiskOverviewTimeSeriesFinding = {
  /**
   * Time bucket start.
   */
  bucketStart: Date;
  /**
   * Policy category key.
   */
  category: string;
  /**
   * Finding count for this category and time bucket.
   */
  findings: number;
};
/** @internal */
export declare const RiskOverviewTimeSeriesFinding$inboundSchema: z.ZodMiniType<
  RiskOverviewTimeSeriesFinding,
  unknown
>;
export declare function riskOverviewTimeSeriesFindingFromJSON(
  jsonString: string,
): SafeParseResult<RiskOverviewTimeSeriesFinding, SDKValidationError>;
//# sourceMappingURL=riskoverviewtimeseriesfinding.d.ts.map
