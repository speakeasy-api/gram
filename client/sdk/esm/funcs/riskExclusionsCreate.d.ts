import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskExclusion } from "../models/components/riskexclusion.js";
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
  CreateRiskExclusionRequest,
  CreateRiskExclusionSecurity,
} from "../models/operations/createriskexclusion.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createRiskExclusion risk
 *
 * @remarks
 * Create a risk exclusion. Omit risk_policy_id to create a global exclusion that applies to every policy in the project.
 */
export declare function riskExclusionsCreate(
  client: GramCore,
  request: CreateRiskExclusionRequest,
  security?: CreateRiskExclusionSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    RiskExclusion,
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
//# sourceMappingURL=riskExclusionsCreate.d.ts.map
