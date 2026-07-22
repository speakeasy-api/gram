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
export type TokenExchangeExchangeMutationVariables = {
  request: operations.TokenExchangeRequest;
  security?: operations.TokenExchangeSecurity | undefined;
  options?: RequestOptions;
};
export type TokenExchangeExchangeMutationData = components.TokenResult;
export type TokenExchangeExchangeMutationError =
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
 * exchange tokenExchange
 *
 * @remarks
 * Exchange the org-scoped device-agent install credential plus a vouched user email for a long-lived, per-user API key carrying the 'agent_user' scope. Authenticated with an org-scoped API key carrying the 'agent' scope — deliberately broader than the 'agent_user' scope the minted per-user keys carry, so a leaked per-user key cannot re-enter this endpoint to forge another user's key. The raw key is returned exactly once.
 */
export declare function useTokenExchangeExchangeMutation(
  options?: MutationHookOptions<
    TokenExchangeExchangeMutationData,
    TokenExchangeExchangeMutationError,
    TokenExchangeExchangeMutationVariables
  >,
): UseMutationResult<
  TokenExchangeExchangeMutationData,
  TokenExchangeExchangeMutationError,
  TokenExchangeExchangeMutationVariables
>;
export declare function mutationKeyTokenExchangeExchange(): MutationKey;
export declare function buildTokenExchangeExchangeMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: TokenExchangeExchangeMutationVariables,
  ) => Promise<TokenExchangeExchangeMutationData>;
};
//# sourceMappingURL=tokenExchangeExchange.d.ts.map
