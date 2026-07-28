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
import { MutationHookOptions } from "./_types.js";
export type TokenExchangeRefreshMutationVariables = {
  request: components.RefreshRequestBody;
  options?: RequestOptions;
};
export type TokenExchangeRefreshMutationData = components.TokenResult;
export type TokenExchangeRefreshMutationError =
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
 * refresh tokenExchange
 *
 * @remarks
 * Rotate a refresh token for a fresh user-scoped access JWT and a newly rotated (single-use) refresh token. The opaque refresh token in the body is self-authenticating; no API key is required.
 */
export declare function useTokenExchangeRefreshMutation(
  options?: MutationHookOptions<
    TokenExchangeRefreshMutationData,
    TokenExchangeRefreshMutationError,
    TokenExchangeRefreshMutationVariables
  >,
): UseMutationResult<
  TokenExchangeRefreshMutationData,
  TokenExchangeRefreshMutationError,
  TokenExchangeRefreshMutationVariables
>;
export declare function mutationKeyTokenExchangeRefresh(): MutationKey;
export declare function buildTokenExchangeRefreshMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: TokenExchangeRefreshMutationVariables,
  ) => Promise<TokenExchangeRefreshMutationData>;
};
//# sourceMappingURL=tokenExchangeRefresh.d.ts.map
