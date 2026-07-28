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
  CloneClientFromOAuthProxyProviderRequest,
  CloneClientFromOAuthProxyProviderSecurity,
} from "../models/operations/cloneclientfromoauthproxyprovider.js";
import { MutationHookOptions } from "./_types.js";
export type CloneClientFromOAuthProxyProviderMutationVariables = {
  request: CloneClientFromOAuthProxyProviderRequest;
  security?: CloneClientFromOAuthProxyProviderSecurity | undefined;
  options?: RequestOptions;
};
export type CloneClientFromOAuthProxyProviderMutationData = RemoteSessionClient;
export type CloneClientFromOAuthProxyProviderMutationError =
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
 * cloneClientFromOAuthProxyProvider remoteSessionClients
 *
 * @remarks
 * Platform-admin-only. Clone the client_id / client_secret from an existing oauth_proxy_provider into a new remote_session_client paired with the supplied issuers. The upstream secret stays server-side: it is read from the proxy provider's stored secrets, re-encrypted, and persisted on the remote_session_client row without ever crossing the wire.
 */
export declare function useCloneClientFromOAuthProxyProviderMutation(
  options?: MutationHookOptions<
    CloneClientFromOAuthProxyProviderMutationData,
    CloneClientFromOAuthProxyProviderMutationError,
    CloneClientFromOAuthProxyProviderMutationVariables
  >,
): UseMutationResult<
  CloneClientFromOAuthProxyProviderMutationData,
  CloneClientFromOAuthProxyProviderMutationError,
  CloneClientFromOAuthProxyProviderMutationVariables
>;
export declare function mutationKeyCloneClientFromOAuthProxyProvider(): MutationKey;
export declare function buildCloneClientFromOAuthProxyProviderMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CloneClientFromOAuthProxyProviderMutationVariables,
  ) => Promise<CloneClientFromOAuthProxyProviderMutationData>;
};
//# sourceMappingURL=cloneClientFromOAuthProxyProvider.d.ts.map
