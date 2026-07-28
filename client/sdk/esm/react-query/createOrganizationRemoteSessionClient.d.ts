import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
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
  CreateOrganizationRemoteSessionClientRequest,
  CreateOrganizationRemoteSessionClientSecurity,
} from "../models/operations/createorganizationremotesessionclient.js";
import { MutationHookOptions } from "./_types.js";
export type CreateOrganizationRemoteSessionClientMutationVariables = {
  request: CreateOrganizationRemoteSessionClientRequest;
  security?: CreateOrganizationRemoteSessionClientSecurity | undefined;
  options?: RequestOptions;
};
export type CreateOrganizationRemoteSessionClientMutationData =
  RemoteSessionClient;
export type CreateOrganizationRemoteSessionClientMutationError =
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
 * createClient organizationRemoteSessionClients
 *
 * @remarks
 * Register a standalone remote_session_client under an existing remote_session_issuer in the caller's organization, with no user_session_issuer attachments. The client is project-scoped: it inherits a project-specific issuer's project, or the caller names a project (which must belong to the organization) when the issuer is organization-level. Requires org:admin.
 */
export declare function useCreateOrganizationRemoteSessionClientMutation(
  options?: MutationHookOptions<
    CreateOrganizationRemoteSessionClientMutationData,
    CreateOrganizationRemoteSessionClientMutationError,
    CreateOrganizationRemoteSessionClientMutationVariables
  >,
): UseMutationResult<
  CreateOrganizationRemoteSessionClientMutationData,
  CreateOrganizationRemoteSessionClientMutationError,
  CreateOrganizationRemoteSessionClientMutationVariables
>;
export declare function mutationKeyCreateOrganizationRemoteSessionClient(): MutationKey;
export declare function buildCreateOrganizationRemoteSessionClientMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateOrganizationRemoteSessionClientMutationVariables,
  ) => Promise<CreateOrganizationRemoteSessionClientMutationData>;
};
//# sourceMappingURL=createOrganizationRemoteSessionClient.d.ts.map
