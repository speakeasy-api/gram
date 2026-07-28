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
  UpdateOrganizationRemoteSessionIssuerRequest,
  UpdateOrganizationRemoteSessionIssuerSecurity,
} from "../models/operations/updateorganizationremotesessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateOrganizationRemoteSessionIssuerMutationVariables = {
  request: UpdateOrganizationRemoteSessionIssuerRequest;
  security?: UpdateOrganizationRemoteSessionIssuerSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateOrganizationRemoteSessionIssuerMutationData =
  RemoteSessionIssuer;
export type UpdateOrganizationRemoteSessionIssuerMutationError =
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
 * updateIssuer organizationRemoteSessionIssuers
 *
 * @remarks
 * Update any remote_session_issuer (organizational or project-specific) in the caller's organization. Requires org:admin.
 */
export declare function useUpdateOrganizationRemoteSessionIssuerMutation(
  options?: MutationHookOptions<
    UpdateOrganizationRemoteSessionIssuerMutationData,
    UpdateOrganizationRemoteSessionIssuerMutationError,
    UpdateOrganizationRemoteSessionIssuerMutationVariables
  >,
): UseMutationResult<
  UpdateOrganizationRemoteSessionIssuerMutationData,
  UpdateOrganizationRemoteSessionIssuerMutationError,
  UpdateOrganizationRemoteSessionIssuerMutationVariables
>;
export declare function mutationKeyUpdateOrganizationRemoteSessionIssuer(): MutationKey;
export declare function buildUpdateOrganizationRemoteSessionIssuerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateOrganizationRemoteSessionIssuerMutationVariables,
  ) => Promise<UpdateOrganizationRemoteSessionIssuerMutationData>;
};
//# sourceMappingURL=updateOrganizationRemoteSessionIssuer.d.ts.map
