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
  CreateGlobalRemoteSessionIssuerRequest,
  CreateGlobalRemoteSessionIssuerSecurity,
} from "../models/operations/createglobalremotesessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type CreateGlobalRemoteSessionIssuerMutationVariables = {
  request: CreateGlobalRemoteSessionIssuerRequest;
  security?: CreateGlobalRemoteSessionIssuerSecurity | undefined;
  options?: RequestOptions;
};
export type CreateGlobalRemoteSessionIssuerMutationData = RemoteSessionIssuer;
export type CreateGlobalRemoteSessionIssuerMutationError =
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
 * createGlobalIssuer adminRemoteSessions
 *
 * @remarks
 * Create a global remote_session_issuer (project_id NULL, organization_id NULL). Requires platform admin.
 */
export declare function useCreateGlobalRemoteSessionIssuerMutation(
  options?: MutationHookOptions<
    CreateGlobalRemoteSessionIssuerMutationData,
    CreateGlobalRemoteSessionIssuerMutationError,
    CreateGlobalRemoteSessionIssuerMutationVariables
  >,
): UseMutationResult<
  CreateGlobalRemoteSessionIssuerMutationData,
  CreateGlobalRemoteSessionIssuerMutationError,
  CreateGlobalRemoteSessionIssuerMutationVariables
>;
export declare function mutationKeyCreateGlobalRemoteSessionIssuer(): MutationKey;
export declare function buildCreateGlobalRemoteSessionIssuerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateGlobalRemoteSessionIssuerMutationVariables,
  ) => Promise<CreateGlobalRemoteSessionIssuerMutationData>;
};
//# sourceMappingURL=createGlobalRemoteSessionIssuer.d.ts.map
