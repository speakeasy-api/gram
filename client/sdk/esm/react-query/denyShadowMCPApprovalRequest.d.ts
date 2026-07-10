import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ShadowMCPApprovalDecisionResult } from "../models/components/shadowmcpapprovaldecisionresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DenyShadowMCPApprovalRequestRequest, DenyShadowMCPApprovalRequestSecurity } from "../models/operations/denyshadowmcpapprovalrequest.js";
import { MutationHookOptions } from "./_types.js";
export type DenyShadowMCPApprovalRequestMutationVariables = {
    request: DenyShadowMCPApprovalRequestRequest;
    security?: DenyShadowMCPApprovalRequestSecurity | undefined;
    options?: RequestOptions;
};
export type DenyShadowMCPApprovalRequestMutationData = ShadowMCPApprovalDecisionResult;
export type DenyShadowMCPApprovalRequestMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * denyShadowMCPApprovalRequest access
 *
 * @remarks
 * Deny a Shadow MCP request and optionally create a deny rule.
 */
export declare function useDenyShadowMCPApprovalRequestMutation(options?: MutationHookOptions<DenyShadowMCPApprovalRequestMutationData, DenyShadowMCPApprovalRequestMutationError, DenyShadowMCPApprovalRequestMutationVariables>): UseMutationResult<DenyShadowMCPApprovalRequestMutationData, DenyShadowMCPApprovalRequestMutationError, DenyShadowMCPApprovalRequestMutationVariables>;
export declare function mutationKeyDenyShadowMCPApprovalRequest(): MutationKey;
export declare function buildDenyShadowMCPApprovalRequestMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DenyShadowMCPApprovalRequestMutationVariables) => Promise<DenyShadowMCPApprovalRequestMutationData>;
};
//# sourceMappingURL=denyShadowMCPApprovalRequest.d.ts.map