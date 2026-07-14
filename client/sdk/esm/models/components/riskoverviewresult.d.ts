import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RiskOverviewCategory } from "./riskoverviewcategory.js";
import { RiskOverviewTimeSeriesFinding } from "./riskoverviewtimeseriesfinding.js";
import { RiskOverviewUser } from "./riskoverviewuser.js";
import { RiskRuleBreakdownEntry } from "./riskrulebreakdownentry.js";
export type RiskOverviewResult = {
  /**
   * Enabled risk policies for the current project.
   */
  activePolicies: number;
  /**
   * Policy findings in the window.
   */
  findings: number;
  /**
   * Chat sessions with at least one finding in the window.
   */
  flaggedSessions: number;
  /**
   * Inclusive start of the overview window.
   */
  from: Date;
  /**
   * Messages analyzed by risk policies in the window.
   */
  messagesScanned: number;
  /**
   * Time-series finding counts by category in the window.
   */
  timeSeriesFindings: Array<RiskOverviewTimeSeriesFinding>;
  /**
   * Exclusive end of the overview window.
   */
  to: Date;
  /**
   * Top policy categories by finding count.
   */
  topCategories: Array<RiskOverviewCategory>;
  /**
   * Top rule_ids by finding count.
   */
  topRules: Array<RiskRuleBreakdownEntry>;
  /**
   * Top users by finding count.
   */
  topUsers: Array<RiskOverviewUser>;
};
/** @internal */
export declare const RiskOverviewResult$inboundSchema: z.ZodMiniType<
  RiskOverviewResult,
  unknown
>;
export declare function riskOverviewResultFromJSON(
  jsonString: string,
): SafeParseResult<RiskOverviewResult, SDKValidationError>;
//# sourceMappingURL=riskoverviewresult.d.ts.map
