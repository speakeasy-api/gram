import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskPolicyBypassRequest } from "../models/components/riskpolicybypassrequest.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ApproveRiskPolicyBypassRequestRequest, ApproveRiskPolicyBypassRequestSecurity } from "../models/operations/approveriskpolicybypassrequest.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * approveRiskPolicyBypassRequest risk
 *
 * @remarks
 * Approve a risk policy bypass request for the requested policy target.
 */
export declare function riskPolicyBypassRequestsApprove(client: GramCore, request: ApproveRiskPolicyBypassRequestRequest, security?: ApproveRiskPolicyBypassRequestSecurity | undefined, options?: RequestOptions): APIPromise<Result<RiskPolicyBypassRequest, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskPolicyBypassRequestsApprove.d.ts.map