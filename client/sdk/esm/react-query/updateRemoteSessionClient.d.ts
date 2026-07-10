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
  UpdateRemoteSessionClientRequest,
  UpdateRemoteSessionClientSecurity,
} from "../models/operations/updateremotesessionclient.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateRemoteSessionClientMutationVariables = {
  request: UpdateRemoteSessionClientRequest;
  security?: UpdateRemoteSessionClientSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateRemoteSessionClientMutationData = RemoteSessionClient;
export type UpdateRemoteSessionClientMutationError =
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
 * updateRemoteSessionClient remoteSessionClients
 *
 * @remarks
 * Rotate the client_secret or change the non-issuer settings on an existing remote_session_client. Issuer attachments are managed via attachUserSessionIssuer / detachUserSessionIssuer.
 */
export declare function useUpdateRemoteSessionClientMutation(
  options?: MutationHookOptions<
    UpdateRemoteSessionClientMutationData,
    UpdateRemoteSessionClientMutationError,
    UpdateRemoteSessionClientMutationVariables
  >,
): UseMutationResult<
  UpdateRemoteSessionClientMutationData,
  UpdateRemoteSessionClientMutationError,
  UpdateRemoteSessionClientMutationVariables
>;
export declare function mutationKeyUpdateRemoteSessionClient(): MutationKey;
export declare function buildUpdateRemoteSessionClientMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateRemoteSessionClientMutationVariables,
  ) => Promise<UpdateRemoteSessionClientMutationData>;
};
//# sourceMappingURL=updateRemoteSessionClient.d.ts.map
