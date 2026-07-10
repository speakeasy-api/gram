import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskPolicy } from "../models/components/riskpolicy.js";
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
  CreateRiskPolicyRequest,
  CreateRiskPolicySecurity,
} from "../models/operations/createriskpolicy.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createRiskPolicy risk
 *
 * @remarks
 * Create a new risk analysis policy for the current project.
 */
export declare function riskPoliciesCreate(
  client: GramCore,
  request: CreateRiskPolicyRequest,
  security?: CreateRiskPolicySecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    RiskPolicy,
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
//# sourceMappingURL=riskPoliciesCreate.d.ts.map
