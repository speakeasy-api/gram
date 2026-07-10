import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * One canonical risk category and how findings are classified into it.
 */
export type RiskCategoryDefinition = {
  /**
   * Plain-English description of what this category covers.
   */
  description: string;
  /**
   * Lucide icon name suggested for this category.
   */
  icon: string;
  /**
   * Canonical category key (e.g. 'secrets', 'pii', 'shadow_mcp').
   */
  key: string;
  /**
   * Human-readable category label for UI rendering.
   */
  label: string;
  /**
   * When non-empty, findings whose rule_id starts with this prefix belong to this category. The catch-all for a family (e.g. 'pii.').
   */
  ruleIdPrefix: string;
  /**
   * When non-empty, findings whose rule_id is in this exact list belong to this category. Checked before rule_id_prefix.
   */
  ruleIds: Array<string>;
  /**
   * When non-empty, findings whose source equals this value belong to this category.
   */
  source: string;
};
/** @internal */
export declare const RiskCategoryDefinition$inboundSchema: z.ZodMiniType<
  RiskCategoryDefinition,
  unknown
>;
export declare function riskCategoryDefinitionFromJSON(
  jsonString: string,
): SafeParseResult<RiskCategoryDefinition, SDKValidationError>;
//# sourceMappingURL=riskcategorydefinition.d.ts.map
