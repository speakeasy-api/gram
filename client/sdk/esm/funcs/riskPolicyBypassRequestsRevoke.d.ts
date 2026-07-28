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
  RevokeRiskPolicyBypassRequestRequest,
  RevokeRiskPolicyBypassRequestSecurity,
} from "../models/operations/revokeriskpolicybypassrequest.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * revokeRiskPolicyBypassRequest risk
 *
 * @remarks
 * Revoke a previously approved risk policy bypass request.
 */
export declare function riskPolicyBypassRequestsRevoke(
  client: GramCore,
  request: RevokeRiskPolicyBypassRequestRequest,
  security?: RevokeRiskPolicyBypassRequestSecurity | undefined,
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
//# sourceMappingURL=riskPolicyBypassRequestsRevoke.d.ts.map
