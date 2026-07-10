import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskPolicy } from "../models/components/riskpolicy.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetRiskPolicyRequest, GetRiskPolicySecurity } from "../models/operations/getriskpolicy.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getRiskPolicy risk
 *
 * @remarks
 * Get a risk analysis policy by ID.
 */
export declare function riskPoliciesGet(client: GramCore, request: GetRiskPolicyRequest, security?: GetRiskPolicySecurity | undefined, options?: RequestOptions): APIPromise<Result<RiskPolicy, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskPoliciesGet.d.ts.map