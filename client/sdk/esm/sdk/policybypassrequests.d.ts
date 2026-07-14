import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListRiskPolicyBypassRequestsResult } from "../models/components/listriskpolicybypassrequestsresult.js";
import { RiskPolicyBypassRequest } from "../models/components/riskpolicybypassrequest.js";
import {
  ApproveRiskPolicyBypassRequestRequest,
  ApproveRiskPolicyBypassRequestSecurity,
} from "../models/operations/approveriskpolicybypassrequest.js";
import {
  CreateRiskPolicyBypassRequestRequest,
  CreateRiskPolicyBypassRequestSecurity,
} from "../models/operations/createriskpolicybypassrequest.js";
import {
  DenyRiskPolicyBypassRequestRequest,
  DenyRiskPolicyBypassRequestSecurity,
} from "../models/operations/denyriskpolicybypassrequest.js";
import {
  ListRiskPolicyBypassRequestsRequest,
  ListRiskPolicyBypassRequestsSecurity,
} from "../models/operations/listriskpolicybypassrequests.js";
import {
  RevokeRiskPolicyBypassRequestRequest,
  RevokeRiskPolicyBypassRequestSecurity,
} from "../models/operations/revokeriskpolicybypassrequest.js";
export declare class PolicyBypassRequests extends ClientSDK {
  /**
   * approveRiskPolicyBypassRequest risk
   *
   * @remarks
   * Approve a risk policy bypass request for the requested policy target.
   */
  approve(
    request: ApproveRiskPolicyBypassRequestRequest,
    security?: ApproveRiskPolicyBypassRequestSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RiskPolicyBypassRequest>;
  /**
   * createRiskPolicyBypassRequest risk
   *
   * @remarks
   * Create or refresh a risk policy bypass request from a signed request URL token.
   */
  create(
    request: CreateRiskPolicyBypassRequestRequest,
    security?: CreateRiskPolicyBypassRequestSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RiskPolicyBypassRequest>;
  /**
   * denyRiskPolicyBypassRequest risk
   *
   * @remarks
   * Deny a risk policy bypass request, updating workflow state.
   */
  deny(
    request: DenyRiskPolicyBypassRequestRequest,
    security?: DenyRiskPolicyBypassRequestSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RiskPolicyBypassRequest>;
  /**
   * listRiskPolicyBypassRequests risk
   *
   * @remarks
   * List current risk policy bypass request workflow records.
   */
  list(
    request?: ListRiskPolicyBypassRequestsRequest | undefined,
    security?: ListRiskPolicyBypassRequestsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListRiskPolicyBypassRequestsResult>;
  /**
   * revokeRiskPolicyBypassRequest risk
   *
   * @remarks
   * Revoke a previously approved risk policy bypass request.
   */
  revoke(
    request: RevokeRiskPolicyBypassRequestRequest,
    security?: RevokeRiskPolicyBypassRequestSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RiskPolicyBypassRequest>;
}
//# sourceMappingURL=policybypassrequests.d.ts.map
