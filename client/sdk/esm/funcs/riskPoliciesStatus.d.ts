import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskPolicyStatus } from "../models/components/riskpolicystatus.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  GetRiskPolicyStatusRequest,
  GetRiskPolicyStatusSecurity,
} from "../models/operations/getriskpolicystatus.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getRiskPolicyStatus risk
 *
 * @remarks
 * Get the analysis status of a risk policy including progress and workflow state.
 */
export declare function riskPoliciesStatus(
  client: GramCore,
  request: GetRiskPolicyStatusRequest,
  security?: GetRiskPolicyStatusSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    RiskPolicyStatus,
    | ServiceError
    | GramError
    | ResponseValidationError
    | ConnectionError
    | RequestAbortedError
    | RequestTimeoutError
    | InvalidRequestError
    | UnexpectedClientError
    | SDKValidationError
  >
>;
//# sourceMappingURL=riskPoliciesStatus.d.ts.map
