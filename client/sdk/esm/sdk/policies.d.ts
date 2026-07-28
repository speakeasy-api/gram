import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListRiskPoliciesResult } from "../models/components/listriskpoliciesresult.js";
import { RiskPolicy } from "../models/components/riskpolicy.js";
import { RiskPolicyStatus } from "../models/components/riskpolicystatus.js";
import {
  CreateRiskPolicyRequest,
  CreateRiskPolicySecurity,
} from "../models/operations/createriskpolicy.js";
import {
  DeleteRiskPolicyRequest,
  DeleteRiskPolicySecurity,
} from "../models/operations/deleteriskpolicy.js";
import {
  GetRiskPolicyRequest,
  GetRiskPolicySecurity,
} from "../models/operations/getriskpolicy.js";
import {
  GetRiskPolicyStatusRequest,
  GetRiskPolicyStatusSecurity,
} from "../models/operations/getriskpolicystatus.js";
import {
  ListRiskPoliciesRequest,
  ListRiskPoliciesSecurity,
} from "../models/operations/listriskpolicies.js";
import {
  TriggerRiskAnalysisRequest,
  TriggerRiskAnalysisSecurity,
} from "../models/operations/triggerriskanalysis.js";
import {
  UpdateRiskPolicyRequest,
  UpdateRiskPolicySecurity,
} from "../models/operations/updateriskpolicy.js";
export declare class Policies extends ClientSDK {
  /**
   * createRiskPolicy risk
   *
   * @remarks
   * Create a new risk analysis policy for the current project.
   */
  create(
    request: CreateRiskPolicyRequest,
    security?: CreateRiskPolicySecurity | undefined,
    options?: RequestOptions,
  ): Promise<RiskPolicy>;
  /**
   * deleteRiskPolicy risk
   *
   * @remarks
   * Delete a risk analysis policy.
   */
  delete(
    request: DeleteRiskPolicyRequest,
    security?: DeleteRiskPolicySecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * getRiskPolicy risk
   *
   * @remarks
   * Get a risk analysis policy by ID.
   */
  get(
    request: GetRiskPolicyRequest,
    security?: GetRiskPolicySecurity | undefined,
    options?: RequestOptions,
  ): Promise<RiskPolicy>;
  /**
   * getRiskPolicyStatus risk
   *
   * @remarks
   * Get the analysis status of a risk policy including progress and workflow state.
   */
  status(
    request: GetRiskPolicyStatusRequest,
    security?: GetRiskPolicyStatusSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RiskPolicyStatus>;
  /**
   * listRiskPolicies risk
   *
   * @remarks
   * List all risk analysis policies for the current project.
   */
  list(
    request?: ListRiskPoliciesRequest | undefined,
    security?: ListRiskPoliciesSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListRiskPoliciesResult>;
  /**
   * triggerRiskAnalysis risk
   *
   * @remarks
   * Manually trigger risk analysis for a policy, starting or signaling the drain workflow. Defaults to the most recent 100 unanalyzed messages; pass `limit=0` to backfill every unanalyzed message.
   */
  trigger(
    request: TriggerRiskAnalysisRequest,
    security?: TriggerRiskAnalysisSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * updateRiskPolicy risk
   *
   * @remarks
   * Update a risk analysis policy.
   */
  update(
    request: UpdateRiskPolicyRequest,
    security?: UpdateRiskPolicySecurity | undefined,
    options?: RequestOptions,
  ): Promise<RiskPolicy>;
}
//# sourceMappingURL=policies.d.ts.map
