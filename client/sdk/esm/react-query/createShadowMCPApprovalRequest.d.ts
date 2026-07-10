import { MutationKey, UseMutationResult } from "@tanstack/react-query";
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
import { MutationHookOptions } from "./_types.js";
export type CreateShadowMCPApprovalRequestMutationVariables = {
  request: CreateShadowMCPApprovalRequestRequest;
  security?: CreateShadowMCPApprovalRequestSecurity | undefined;
  options?: RequestOptions;
};
export type CreateShadowMCPApprovalRequestMutationData =
  ShadowMCPApprovalRequest;
export type CreateShadowMCPApprovalRequestMutationError =
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
 * createShadowMCPApprovalRequest access
 *
 * @remarks
 * Create or return an active Shadow MCP approval request.
 */
export declare function useCreateShadowMCPApprovalRequestMutation(
  options?: MutationHookOptions<
    CreateShadowMCPApprovalRequestMutationData,
    CreateShadowMCPApprovalRequestMutationError,
    CreateShadowMCPApprovalRequestMutationVariables
  >,
): UseMutationResult<
  CreateShadowMCPApprovalRequestMutationData,
  CreateShadowMCPApprovalRequestMutationError,
  CreateShadowMCPApprovalRequestMutationVariables
>;
export declare function mutationKeyCreateShadowMCPApprovalRequest(): MutationKey;
export declare function buildCreateShadowMCPApprovalRequestMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateShadowMCPApprovalRequestMutationVariables,
  ) => Promise<CreateShadowMCPApprovalRequestMutationData>;
};
//# sourceMappingURL=createShadowMCPApprovalRequest.d.ts.map
