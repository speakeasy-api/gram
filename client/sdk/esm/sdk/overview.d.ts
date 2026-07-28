import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { RiskOverviewResult } from "../models/components/riskoverviewresult.js";
import { RiskRuleBreakdownResult } from "../models/components/riskrulebreakdownresult.js";
import { RiskUserBreakdownResult } from "../models/components/riskuserbreakdownresult.js";
import {
  GetRiskOverviewRequest,
  GetRiskOverviewSecurity,
} from "../models/operations/getriskoverview.js";
import {
  GetRiskRuleBreakdownRequest,
  GetRiskRuleBreakdownSecurity,
} from "../models/operations/getriskrulebreakdown.js";
import {
  GetRiskUserBreakdownRequest,
  GetRiskUserBreakdownSecurity,
} from "../models/operations/getriskuserbreakdown.js";
export declare class Overview extends ClientSDK {
  /**
   * getRiskOverview risk
   *
   * @remarks
   * Get risk overview metrics and trend data for the current project.
   */
  get(
    request?: GetRiskOverviewRequest | undefined,
    security?: GetRiskOverviewSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RiskOverviewResult>;
  /**
   * getRiskRuleBreakdown risk
   *
   * @remarks
   * Get per-rule_id finding counts for a category within a time window. Powers the per-category drill-down chart on /risk-overview.
   */
  rules(
    request: GetRiskRuleBreakdownRequest,
    security?: GetRiskRuleBreakdownSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RiskRuleBreakdownResult>;
  /**
   * getRiskUserBreakdown risk
   *
   * @remarks
   * Per-user breakdowns of findings by category and by rule_id within a time window. Powers the user drill-down on /risk-overview.
   */
  userBreakdown(
    request: GetRiskUserBreakdownRequest,
    security?: GetRiskUserBreakdownSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RiskUserBreakdownResult>;
}
//# sourceMappingURL=overview.d.ts.map
