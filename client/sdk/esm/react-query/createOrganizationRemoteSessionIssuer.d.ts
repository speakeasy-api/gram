import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionIssuer } from "../models/components/remotesessionissuer.js";
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
  CreateOrganizationRemoteSessionIssuerRequest,
  CreateOrganizationRemoteSessionIssuerSecurity,
} from "../models/operations/createorganizationremotesessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type CreateOrganizationRemoteSessionIssuerMutationVariables = {
  request: CreateOrganizationRemoteSessionIssuerRequest;
  security?: CreateOrganizationRemoteSessionIssuerSecurity | undefined;
  options?: RequestOptions;
};
export type CreateOrganizationRemoteSessionIssuerMutationData =
  RemoteSessionIssuer;
export type CreateOrganizationRemoteSessionIssuerMutationError =
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
 * createIssuer organizationRemoteSessionIssuers
 *
 * @remarks
 * Create a remote_session_issuer in the caller's organization. With no project_id the issuer is organization-level (project_id NULL, inherited by every project); with a project_id (which must belong to the organization) it is project-specific. Requires org:admin.
 */
export declare function useCreateOrganizationRemoteSessionIssuerMutation(
  options?: MutationHookOptions<
    CreateOrganizationRemoteSessionIssuerMutationData,
    CreateOrganizationRemoteSessionIssuerMutationError,
    CreateOrganizationRemoteSessionIssuerMutationVariables
  >,
): UseMutationResult<
  CreateOrganizationRemoteSessionIssuerMutationData,
  CreateOrganizationRemoteSessionIssuerMutationError,
  CreateOrganizationRemoteSessionIssuerMutationVariables
>;
export declare function mutationKeyCreateOrganizationRemoteSessionIssuer(): MutationKey;
export declare function buildCreateOrganizationRemoteSessionIssuerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateOrganizationRemoteSessionIssuerMutationVariables,
  ) => Promise<CreateOrganizationRemoteSessionIssuerMutationData>;
};
//# sourceMappingURL=createOrganizationRemoteSessionIssuer.d.ts.map
