import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRiskExclusionsResult } from "../models/components/listriskexclusionsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListRiskExclusionsRequest, ListRiskExclusionsSecurity } from "../models/operations/listriskexclusions.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listRiskExclusions risk
 *
 * @remarks
 * List risk exclusions for the current project. Optionally filter to a single policy.
 */
export declare function riskExclusionsList(client: GramCore, request?: ListRiskExclusionsRequest | undefined, security?: ListRiskExclusionsSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListRiskExclusionsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskExclusionsList.d.ts.map