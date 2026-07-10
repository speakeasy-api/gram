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
  DeleteGlobalRemoteSessionIssuerRequest,
  DeleteGlobalRemoteSessionIssuerSecurity,
} from "../models/operations/deleteglobalremotesessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteGlobalRemoteSessionIssuerMutationVariables = {
  request: DeleteGlobalRemoteSessionIssuerRequest;
  security?: DeleteGlobalRemoteSessionIssuerSecurity | undefined;
  options?: RequestOptions;
};
export type DeleteGlobalRemoteSessionIssuerMutationData = void;
export type DeleteGlobalRemoteSessionIssuerMutationError =
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
 * deleteGlobalIssuer adminRemoteSessions
 *
 * @remarks
 * Soft-delete a global remote_session_issuer. Blocked when any global remote_session_clients still reference it. Requires platform admin.
 */
export declare function useDeleteGlobalRemoteSessionIssuerMutation(
  options?: MutationHookOptions<
    DeleteGlobalRemoteSessionIssuerMutationData,
    DeleteGlobalRemoteSessionIssuerMutationError,
    DeleteGlobalRemoteSessionIssuerMutationVariables
  >,
): UseMutationResult<
  DeleteGlobalRemoteSessionIssuerMutationData,
  DeleteGlobalRemoteSessionIssuerMutationError,
  DeleteGlobalRemoteSessionIssuerMutationVariables
>;
export declare function mutationKeyDeleteGlobalRemoteSessionIssuer(): MutationKey;
export declare function buildDeleteGlobalRemoteSessionIssuerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: DeleteGlobalRemoteSessionIssuerMutationVariables,
  ) => Promise<DeleteGlobalRemoteSessionIssuerMutationData>;
};
//# sourceMappingURL=deleteGlobalRemoteSessionIssuer.d.ts.map
