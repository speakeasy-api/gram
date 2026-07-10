import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RiskResult } from "./riskresult.js";
export type ListRiskResultsResult = {
  /**
   * Cursor for the next page of results.
   */
  nextCursor?: string | undefined;
  /**
   * The list of risk results.
   */
  results: Array<RiskResult>;
  /**
   * Total number of findings across all enabled policies.
   */
  totalCount: number;
};
/** @internal */
export declare const ListRiskResultsResult$inboundSchema: z.ZodMiniType<
  ListRiskResultsResult,
  unknown
>;
export declare function listRiskResultsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListRiskResultsResult, SDKValidationError>;
//# sourceMappingURL=listriskresultsresult.d.ts.map
