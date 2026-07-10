import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RiskExclusion } from "./riskexclusion.js";
export type ListRiskExclusionsResult = {
  /**
   * The list of risk exclusions.
   */
  exclusions: Array<RiskExclusion>;
};
/** @internal */
export declare const ListRiskExclusionsResult$inboundSchema: z.ZodMiniType<
  ListRiskExclusionsResult,
  unknown
>;
export declare function listRiskExclusionsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListRiskExclusionsResult, SDKValidationError>;
//# sourceMappingURL=listriskexclusionsresult.d.ts.map
