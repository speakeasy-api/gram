import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListShadowMCPApprovalRequestsResult } from "../models/components/listshadowmcpapprovalrequestsresult.js";
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
  ListShadowMCPApprovalRequestsRequest,
  ListShadowMCPApprovalRequestsSecurity,
} from "../models/operations/listshadowmcpapprovalrequests.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listShadowMCPApprovalRequests access
 *
 * @remarks
 * List Shadow MCP approval requests for the current organization. Requires organization admin access because requests include requester and block details.
 */
export declare function accessListShadowMCPApprovalRequests(
  client: GramCore,
  request?: ListShadowMCPApprovalRequestsRequest | undefined,
  security?: ListShadowMCPApprovalRequestsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListShadowMCPApprovalRequestsResult,
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
//# sourceMappingURL=accessListShadowMCPApprovalRequests.d.ts.map
