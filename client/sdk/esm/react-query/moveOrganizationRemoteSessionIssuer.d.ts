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
  MoveOrganizationRemoteSessionIssuerRequest,
  MoveOrganizationRemoteSessionIssuerSecurity,
} from "../models/operations/moveorganizationremotesessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type MoveOrganizationRemoteSessionIssuerMutationVariables = {
  request: MoveOrganizationRemoteSessionIssuerRequest;
  security?: MoveOrganizationRemoteSessionIssuerSecurity | undefined;
  options?: RequestOptions;
};
export type MoveOrganizationRemoteSessionIssuerMutationData =
  RemoteSessionIssuer;
export type MoveOrganizationRemoteSessionIssuerMutationError =
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
 * moveIssuer organizationRemoteSessionIssuers
 *
 * @remarks
 * Re-scope a remote_session_issuer in the caller's organization: provide a project_id (which must belong to the organization) to make it project-specific, or omit it to make it organization-level (project_id NULL, inherited by every project). Requires org:admin.
 */
export declare function useMoveOrganizationRemoteSessionIssuerMutation(
  options?: MutationHookOptions<
    MoveOrganizationRemoteSessionIssuerMutationData,
    MoveOrganizationRemoteSessionIssuerMutationError,
    MoveOrganizationRemoteSessionIssuerMutationVariables
  >,
): UseMutationResult<
  MoveOrganizationRemoteSessionIssuerMutationData,
  MoveOrganizationRemoteSessionIssuerMutationError,
  MoveOrganizationRemoteSessionIssuerMutationVariables
>;
export declare function mutationKeyMoveOrganizationRemoteSessionIssuer(): MutationKey;
export declare function buildMoveOrganizationRemoteSessionIssuerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: MoveOrganizationRemoteSessionIssuerMutationVariables,
  ) => Promise<MoveOrganizationRemoteSessionIssuerMutationData>;
};
//# sourceMappingURL=moveOrganizationRemoteSessionIssuer.d.ts.map
