import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listShadowMCPApprovals risk
 *
 * @remarks
 * List shadow-MCP approvals (URL- or command-keyed) for a policy. Temporary Redis-backed storage; will move to a dedicated table once the feature graduates.
 */
export declare function riskApprovalsList(
  client: GramCore,
  request: operations.ListShadowMCPApprovalsRequest,
  security?: operations.ListShadowMCPApprovalsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    components.ListShadowMCPApprovalsResult,
    | errors.ServiceError
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
//# sourceMappingURL=riskApprovalsList.d.ts.map
