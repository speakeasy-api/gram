import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ShadowMCPApprovalDecisionResult } from "../models/components/shadowmcpapprovaldecisionresult.js";
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
  ApproveShadowMCPApprovalRequestRequest,
  ApproveShadowMCPApprovalRequestSecurity,
} from "../models/operations/approveshadowmcpapprovalrequest.js";
import { MutationHookOptions } from "./_types.js";
export type ApproveShadowMCPApprovalRequestMutationVariables = {
  request: ApproveShadowMCPApprovalRequestRequest;
  security?: ApproveShadowMCPApprovalRequestSecurity | undefined;
  options?: RequestOptions;
};
export type ApproveShadowMCPApprovalRequestMutationData =
  ShadowMCPApprovalDecisionResult;
export type ApproveShadowMCPApprovalRequestMutationError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * approveShadowMCPApprovalRequest access
 *
 * @remarks
 * Approve a Shadow MCP request, creating an allow rule scoped to the organization or project.
 */
export declare function useApproveShadowMCPApprovalRequestMutation(
  options?: MutationHookOptions<
    ApproveShadowMCPApprovalRequestMutationData,
    ApproveShadowMCPApprovalRequestMutationError,
    ApproveShadowMCPApprovalRequestMutationVariables
  >,
): UseMutationResult<
  ApproveShadowMCPApprovalRequestMutationData,
  ApproveShadowMCPApprovalRequestMutationError,
  ApproveShadowMCPApprovalRequestMutationVariables
>;
export declare function mutationKeyApproveShadowMCPApprovalRequest(): MutationKey;
export declare function buildApproveShadowMCPApprovalRequestMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: ApproveShadowMCPApprovalRequestMutationVariables,
  ) => Promise<ApproveShadowMCPApprovalRequestMutationData>;
};
//# sourceMappingURL=approveShadowMCPApprovalRequest.d.ts.map
