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
  DeleteRemoteSessionIssuerRequest,
  DeleteRemoteSessionIssuerSecurity,
} from "../models/operations/deleteremotesessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteRemoteSessionIssuerMutationVariables = {
  request: DeleteRemoteSessionIssuerRequest;
  security?: DeleteRemoteSessionIssuerSecurity | undefined;
  options?: RequestOptions;
};
export type DeleteRemoteSessionIssuerMutationData = void;
export type DeleteRemoteSessionIssuerMutationError =
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
 * deleteRemoteSessionIssuer remoteSessionIssuers
 *
 * @remarks
 * Soft-delete a remote_session_issuer. Blocked if any remote_session_clients still reference it.
 */
export declare function useDeleteRemoteSessionIssuerMutation(
  options?: MutationHookOptions<
    DeleteRemoteSessionIssuerMutationData,
    DeleteRemoteSessionIssuerMutationError,
    DeleteRemoteSessionIssuerMutationVariables
  >,
): UseMutationResult<
  DeleteRemoteSessionIssuerMutationData,
  DeleteRemoteSessionIssuerMutationError,
  DeleteRemoteSessionIssuerMutationVariables
>;
export declare function mutationKeyDeleteRemoteSessionIssuer(): MutationKey;
export declare function buildDeleteRemoteSessionIssuerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: DeleteRemoteSessionIssuerMutationVariables,
  ) => Promise<DeleteRemoteSessionIssuerMutationData>;
};
//# sourceMappingURL=deleteRemoteSessionIssuer.d.ts.map
