import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ShadowMCPApprovalDecisionResult } from "../models/components/shadowmcpapprovaldecisionresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DenyShadowMCPApprovalRequestRequest, DenyShadowMCPApprovalRequestSecurity } from "../models/operations/denyshadowmcpapprovalrequest.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * denyShadowMCPApprovalRequest access
 *
 * @remarks
 * Deny a Shadow MCP request and optionally create a deny rule.
 */
export declare function accessDenyShadowMCPApprovalRequest(client: GramCore, request: DenyShadowMCPApprovalRequestRequest, security?: DenyShadowMCPApprovalRequestSecurity | undefined, options?: RequestOptions): APIPromise<Result<ShadowMCPApprovalDecisionResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=accessDenyShadowMCPApprovalRequest.d.ts.map