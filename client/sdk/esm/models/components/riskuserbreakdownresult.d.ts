import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RiskOverviewCategory } from "./riskoverviewcategory.js";
import { RiskRuleBreakdownEntry } from "./riskrulebreakdownentry.js";
export type RiskUserBreakdownResult = {
  /**
   * Category breakdown for this user, ordered by finding count descending.
   */
  categories: Array<RiskOverviewCategory>;
  /**
   * External user the breakdown is scoped to.
   */
  externalUserId: string;
  /**
   * Total findings for this user in the window.
   */
  findings: number;
  /**
   * Inclusive start of the window used.
   */
  from: Date;
  /**
   * Rule_id breakdown for this user, ordered by finding count descending.
   */
  rules: Array<RiskRuleBreakdownEntry>;
  /**
   * Exclusive end of the window used.
   */
  to: Date;
};
/** @internal */
export declare const RiskUserBreakdownResult$inboundSchema: z.ZodMiniType<
  RiskUserBreakdownResult,
  unknown
>;
export declare function riskUserBreakdownResultFromJSON(
  jsonString: string,
): SafeParseResult<RiskUserBreakdownResult, SDKValidationError>;
//# sourceMappingURL=riskuserbreakdownresult.d.ts.map
