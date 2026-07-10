import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskPolicyBypassRequest } from "../models/components/riskpolicybypassrequest.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateRiskPolicyBypassRequestRequest, CreateRiskPolicyBypassRequestSecurity } from "../models/operations/createriskpolicybypassrequest.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createRiskPolicyBypassRequest risk
 *
 * @remarks
 * Create or refresh a risk policy bypass request from a signed request URL token.
 */
export declare function riskPolicyBypassRequestsCreate(client: GramCore, request: CreateRiskPolicyBypassRequestRequest, security?: CreateRiskPolicyBypassRequestSecurity | undefined, options?: RequestOptions): APIPromise<Result<RiskPolicyBypassRequest, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskPolicyBypassRequestsCreate.d.ts.map