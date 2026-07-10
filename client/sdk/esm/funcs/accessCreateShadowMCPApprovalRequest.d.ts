import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ShadowMCPApprovalRequest } from "../models/components/shadowmcpapprovalrequest.js";
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
  CreateShadowMCPApprovalRequestRequest,
  CreateShadowMCPApprovalRequestSecurity,
} from "../models/operations/createshadowmcpapprovalrequest.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createShadowMCPApprovalRequest access
 *
 * @remarks
 * Create or return an active Shadow MCP approval request.
 */
export declare function accessCreateShadowMCPApprovalRequest(
  client: GramCore,
  request: CreateShadowMCPApprovalRequestRequest,
  security?: CreateShadowMCPApprovalRequestSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ShadowMCPApprovalRequest,
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
//# sourceMappingURL=accessCreateShadowMCPApprovalRequest.d.ts.map
