import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Toolset } from "../models/components/toolset.js";
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
  RemoveOAuthServerRequest,
  RemoveOAuthServerSecurity,
} from "../models/operations/removeoauthserver.js";
import { MutationHookOptions } from "./_types.js";
export type RemoveOAuthServerMutationVariables = {
  request: RemoveOAuthServerRequest;
  security?: RemoveOAuthServerSecurity | undefined;
  options?: RequestOptions;
};
export type RemoveOAuthServerMutationData = Toolset;
export type RemoveOAuthServerMutationError =
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
 * removeOAuthServer toolsets
 *
 * @remarks
 * Remove OAuth server association from a toolset
 */
export declare function useRemoveOAuthServerMutation(
  options?: MutationHookOptions<
    RemoveOAuthServerMutationData,
    RemoveOAuthServerMutationError,
    RemoveOAuthServerMutationVariables
  >,
): UseMutationResult<
  RemoveOAuthServerMutationData,
  RemoveOAuthServerMutationError,
  RemoveOAuthServerMutationVariables
>;
export declare function mutationKeyRemoveOAuthServer(): MutationKey;
export declare function buildRemoveOAuthServerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RemoveOAuthServerMutationVariables,
  ) => Promise<RemoveOAuthServerMutationData>;
};
//# sourceMappingURL=removeOAuthServer.d.ts.map
