import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskPolicy } from "../models/components/riskpolicy.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateRiskPolicyRequest, UpdateRiskPolicySecurity } from "../models/operations/updateriskpolicy.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateRiskPolicy risk
 *
 * @remarks
 * Update a risk analysis policy.
 */
export declare function riskPoliciesUpdate(client: GramCore, request: UpdateRiskPolicyRequest, security?: UpdateRiskPolicySecurity | undefined, options?: RequestOptions): APIPromise<Result<RiskPolicy, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskPoliciesUpdate.d.ts.map