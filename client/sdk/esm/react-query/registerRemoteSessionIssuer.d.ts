import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type RegisterRemoteSessionIssuerMutationVariables = {
  request: operations.RegisterRemoteSessionIssuerRequest;
  security?: operations.RegisterRemoteSessionIssuerSecurity | undefined;
  options?: RequestOptions;
};
export type RegisterRemoteSessionIssuerMutationData =
  components.RemoteSessionClient;
export type RegisterRemoteSessionIssuerMutationError =
  | errors.ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * registerRemoteSessionIssuer remoteSessionIssuers
 *
 * @remarks
 * Perform an RFC 7591 Dynamic Client Registration call against an existing issuer's registration_endpoint and persist the issued credentials as a new remote_session_client. The issuer must already have a registration_endpoint configured.
 */
export declare function useRegisterRemoteSessionIssuerMutation(
  options?: MutationHookOptions<
    RegisterRemoteSessionIssuerMutationData,
    RegisterRemoteSessionIssuerMutationError,
    RegisterRemoteSessionIssuerMutationVariables
  >,
): UseMutationResult<
  RegisterRemoteSessionIssuerMutationData,
  RegisterRemoteSessionIssuerMutationError,
  RegisterRemoteSessionIssuerMutationVariables
>;
export declare function mutationKeyRegisterRemoteSessionIssuer(): MutationKey;
export declare function buildRegisterRemoteSessionIssuerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RegisterRemoteSessionIssuerMutationVariables,
  ) => Promise<RegisterRemoteSessionIssuerMutationData>;
};
//# sourceMappingURL=registerRemoteSessionIssuer.d.ts.map
