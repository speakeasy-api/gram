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
  RevokeAPIKeyRequest,
  RevokeAPIKeySecurity,
} from "../models/operations/revokeapikey.js";
import { MutationHookOptions } from "./_types.js";
export type RevokeAPIKeyMutationVariables = {
  request: RevokeAPIKeyRequest;
  security?: RevokeAPIKeySecurity | undefined;
  options?: RequestOptions;
};
export type RevokeAPIKeyMutationData = void;
export type RevokeAPIKeyMutationError =
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
 * revokeKey keys
 *
 * @remarks
 * Revoke a api key
 */
export declare function useRevokeAPIKeyMutation(
  options?: MutationHookOptions<
    RevokeAPIKeyMutationData,
    RevokeAPIKeyMutationError,
    RevokeAPIKeyMutationVariables
  >,
): UseMutationResult<
  RevokeAPIKeyMutationData,
  RevokeAPIKeyMutationError,
  RevokeAPIKeyMutationVariables
>;
export declare function mutationKeyRevokeAPIKey(): MutationKey;
export declare function buildRevokeAPIKeyMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RevokeAPIKeyMutationVariables,
  ) => Promise<RevokeAPIKeyMutationData>;
};
//# sourceMappingURL=revokeAPIKey.d.ts.map
