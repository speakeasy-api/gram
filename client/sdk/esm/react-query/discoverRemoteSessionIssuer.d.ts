import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionIssuerDraft } from "../models/components/remotesessionissuerdraft.js";
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
  DiscoverRemoteSessionIssuerRequest,
  DiscoverRemoteSessionIssuerSecurity,
} from "../models/operations/discoverremotesessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type DiscoverRemoteSessionIssuerMutationVariables = {
  request: DiscoverRemoteSessionIssuerRequest;
  security?: DiscoverRemoteSessionIssuerSecurity | undefined;
  options?: RequestOptions;
};
export type DiscoverRemoteSessionIssuerMutationData = RemoteSessionIssuerDraft;
export type DiscoverRemoteSessionIssuerMutationError =
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
 * discoverRemoteSessionIssuer remoteSessionIssuers
 *
 * @remarks
 * Hit an upstream issuer's RFC 8414 .well-known/oauth-authorization-server document and return a draft suitable for createRemoteSessionIssuer. No persistence.
 */
export declare function useDiscoverRemoteSessionIssuerMutation(
  options?: MutationHookOptions<
    DiscoverRemoteSessionIssuerMutationData,
    DiscoverRemoteSessionIssuerMutationError,
    DiscoverRemoteSessionIssuerMutationVariables
  >,
): UseMutationResult<
  DiscoverRemoteSessionIssuerMutationData,
  DiscoverRemoteSessionIssuerMutationError,
  DiscoverRemoteSessionIssuerMutationVariables
>;
export declare function mutationKeyDiscoverRemoteSessionIssuer(): MutationKey;
export declare function buildDiscoverRemoteSessionIssuerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: DiscoverRemoteSessionIssuerMutationVariables,
  ) => Promise<DiscoverRemoteSessionIssuerMutationData>;
};
//# sourceMappingURL=discoverRemoteSessionIssuer.d.ts.map
