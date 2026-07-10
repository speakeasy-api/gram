import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ShadowMCPApprovalDecisionResult } from "../models/components/shadowmcpapprovaldecisionresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ApproveShadowMCPApprovalRequestRequest, ApproveShadowMCPApprovalRequestSecurity } from "../models/operations/approveshadowmcpapprovalrequest.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * approveShadowMCPApprovalRequest access
 *
 * @remarks
 * Approve a Shadow MCP request, creating an allow rule scoped to the organization or project.
 */
export declare function accessApproveShadowMCPApprovalRequest(client: GramCore, request: ApproveShadowMCPApprovalRequestRequest, security?: ApproveShadowMCPApprovalRequestSecurity | undefined, options?: RequestOptions): APIPromise<Result<ShadowMCPApprovalDecisionResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=accessApproveShadowMCPApprovalRequest.d.ts.map