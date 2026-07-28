import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RiskCategoryDefinition } from "./riskcategorydefinition.js";
/**
 * Canonical risk category definitions used to classify findings, in classification-priority order. Consumers should iterate in order and pick the first match.
 */
export type RiskCategoriesResult = {
  /**
   * Categories in classification-priority order. The last entry is the 'custom' fallback for findings that match none of the others.
   */
  categories: Array<RiskCategoryDefinition>;
};
/** @internal */
export declare const RiskCategoriesResult$inboundSchema: z.ZodMiniType<
  RiskCategoriesResult,
  unknown
>;
export declare function riskCategoriesResultFromJSON(
  jsonString: string,
): SafeParseResult<RiskCategoriesResult, SDKValidationError>;
//# sourceMappingURL=riskcategoriesresult.d.ts.map
