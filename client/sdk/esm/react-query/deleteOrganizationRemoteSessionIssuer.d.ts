import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
  DeleteOrganizationRemoteSessionIssuerRequest,
  DeleteOrganizationRemoteSessionIssuerSecurity,
} from "../models/operations/deleteorganizationremotesessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteOrganizationRemoteSessionIssuerMutationVariables = {
  request: DeleteOrganizationRemoteSessionIssuerRequest;
  security?: DeleteOrganizationRemoteSessionIssuerSecurity | undefined;
  options?: RequestOptions;
};
export type DeleteOrganizationRemoteSessionIssuerMutationData = void;
export type DeleteOrganizationRemoteSessionIssuerMutationError =
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
 * deleteIssuer organizationRemoteSessionIssuers
 *
 * @remarks
 * Soft-delete any remote_session_issuer (organizational or project-specific) in the caller's organization. Blocked when any remote_session_clients still reference it. Requires org:admin.
 */
export declare function useDeleteOrganizationRemoteSessionIssuerMutation(
  options?: MutationHookOptions<
    DeleteOrganizationRemoteSessionIssuerMutationData,
    DeleteOrganizationRemoteSessionIssuerMutationError,
    DeleteOrganizationRemoteSessionIssuerMutationVariables
  >,
): UseMutationResult<
  DeleteOrganizationRemoteSessionIssuerMutationData,
  DeleteOrganizationRemoteSessionIssuerMutationError,
  DeleteOrganizationRemoteSessionIssuerMutationVariables
>;
export declare function mutationKeyDeleteOrganizationRemoteSessionIssuer(): MutationKey;
export declare function buildDeleteOrganizationRemoteSessionIssuerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: DeleteOrganizationRemoteSessionIssuerMutationVariables,
  ) => Promise<DeleteOrganizationRemoteSessionIssuerMutationData>;
};
//# sourceMappingURL=deleteOrganizationRemoteSessionIssuer.d.ts.map
