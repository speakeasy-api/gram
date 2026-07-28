import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type RiskOverviewCategory = {
  /**
   * Policy category key.
   */
  category: string;
  /**
   * Finding count for this category.
   */
  findings: number;
};
/** @internal */
export declare const RiskOverviewCategory$inboundSchema: z.ZodMiniType<
  RiskOverviewCategory,
  unknown
>;
export declare function riskOverviewCategoryFromJSON(
  jsonString: string,
): SafeParseResult<RiskOverviewCategory, SDKValidationError>;
//# sourceMappingURL=riskoverviewcategory.d.ts.map
