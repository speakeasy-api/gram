import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRiskPoliciesResult } from "../models/components/listriskpoliciesresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListRiskPoliciesRequest, ListRiskPoliciesSecurity } from "../models/operations/listriskpolicies.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listRiskPolicies risk
 *
 * @remarks
 * List all risk analysis policies for the current project.
 */
export declare function riskPoliciesList(client: GramCore, request?: ListRiskPoliciesRequest | undefined, security?: ListRiskPoliciesSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListRiskPoliciesResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskPoliciesList.d.ts.map