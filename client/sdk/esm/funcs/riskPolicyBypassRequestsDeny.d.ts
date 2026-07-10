import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskPolicyBypassRequest } from "../models/components/riskpolicybypassrequest.js";
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
  DenyRiskPolicyBypassRequestRequest,
  DenyRiskPolicyBypassRequestSecurity,
} from "../models/operations/denyriskpolicybypassrequest.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * denyRiskPolicyBypassRequest risk
 *
 * @remarks
 * Deny a risk policy bypass request, updating workflow state.
 */
export declare function riskPolicyBypassRequestsDeny(
  client: GramCore,
  request: DenyRiskPolicyBypassRequestRequest,
  security?: DenyRiskPolicyBypassRequestSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    RiskPolicyBypassRequest,
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
//# sourceMappingURL=riskPolicyBypassRequestsDeny.d.ts.map
