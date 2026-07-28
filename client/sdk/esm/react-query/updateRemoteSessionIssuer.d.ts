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
  UpdateRemoteSessionIssuerRequest,
  UpdateRemoteSessionIssuerSecurity,
} from "../models/operations/updateremotesessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateRemoteSessionIssuerMutationVariables = {
  request: UpdateRemoteSessionIssuerRequest;
  security?: UpdateRemoteSessionIssuerSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateRemoteSessionIssuerMutationData = RemoteSessionIssuer;
export type UpdateRemoteSessionIssuerMutationError =
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
 * updateRemoteSessionIssuer remoteSessionIssuers
 *
 * @remarks
 * Update fields on an existing remote_session_issuer.
 */
export declare function useUpdateRemoteSessionIssuerMutation(
  options?: MutationHookOptions<
    UpdateRemoteSessionIssuerMutationData,
    UpdateRemoteSessionIssuerMutationError,
    UpdateRemoteSessionIssuerMutationVariables
  >,
): UseMutationResult<
  UpdateRemoteSessionIssuerMutationData,
  UpdateRemoteSessionIssuerMutationError,
  UpdateRemoteSessionIssuerMutationVariables
>;
export declare function mutationKeyUpdateRemoteSessionIssuer(): MutationKey;
export declare function buildUpdateRemoteSessionIssuerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateRemoteSessionIssuerMutationVariables,
  ) => Promise<UpdateRemoteSessionIssuerMutationData>;
};
//# sourceMappingURL=updateRemoteSessionIssuer.d.ts.map
