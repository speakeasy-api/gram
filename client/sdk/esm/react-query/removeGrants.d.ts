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
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type RemoveGrantsMutationVariables = {
  request: operations.RemoveGrantsRequest;
  security?: operations.RemoveGrantsSecurity | undefined;
  options?: RequestOptions;
};
export type RemoveGrantsMutationData = void;
export type RemoveGrantsMutationError =
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
 * removeGrants access
 *
 * @remarks
 * Revoke specific permissions from users or roles. Each entry must exactly match an existing grant (who, what action, which resource).
 */
export declare function useRemoveGrantsMutation(
  options?: MutationHookOptions<
    RemoveGrantsMutationData,
    RemoveGrantsMutationError,
    RemoveGrantsMutationVariables
  >,
): UseMutationResult<
  RemoveGrantsMutationData,
  RemoveGrantsMutationError,
  RemoveGrantsMutationVariables
>;
export declare function mutationKeyRemoveGrants(): MutationKey;
export declare function buildRemoveGrantsMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RemoveGrantsMutationVariables,
  ) => Promise<RemoveGrantsMutationData>;
};
//# sourceMappingURL=removeGrants.d.ts.map
