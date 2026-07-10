import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type RiskRuleBreakdownEntry = {
  /**
   * Finding count for this rule within the window.
   */
  findings: number;
  /**
   * Rule identifier (e.g. 'secret.aws-access-key'). Empty when the finding has no rule_id (treat as 'unspecified').
   */
  ruleId: string;
  /**
   * Source bucket the rule belongs to (gitleaks, presidio, etc.) for label/icon resolution on the dashboard.
   */
  source: string;
};
/** @internal */
export declare const RiskRuleBreakdownEntry$inboundSchema: z.ZodMiniType<
  RiskRuleBreakdownEntry,
  unknown
>;
export declare function riskRuleBreakdownEntryFromJSON(
  jsonString: string,
): SafeParseResult<RiskRuleBreakdownEntry, SDKValidationError>;
//# sourceMappingURL=riskrulebreakdownentry.d.ts.map
