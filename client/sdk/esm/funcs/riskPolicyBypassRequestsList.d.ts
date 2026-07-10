import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRiskPolicyBypassRequestsResult } from "../models/components/listriskpolicybypassrequestsresult.js";
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
  ListRiskPolicyBypassRequestsRequest,
  ListRiskPolicyBypassRequestsSecurity,
} from "../models/operations/listriskpolicybypassrequests.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listRiskPolicyBypassRequests risk
 *
 * @remarks
 * List current risk policy bypass request workflow records.
 */
export declare function riskPolicyBypassRequestsList(
  client: GramCore,
  request?: ListRiskPolicyBypassRequestsRequest | undefined,
  security?: ListRiskPolicyBypassRequestsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListRiskPolicyBypassRequestsResult,
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
//# sourceMappingURL=riskPolicyBypassRequestsList.d.ts.map
