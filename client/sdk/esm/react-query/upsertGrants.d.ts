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
export type UpsertGrantsMutationVariables = {
  request: operations.UpsertGrantsRequest;
  security?: operations.UpsertGrantsSecurity | undefined;
  options?: RequestOptions;
};
export type UpsertGrantsMutationData = components.UpsertGrantsResult;
export type UpsertGrantsMutationError =
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
 * upsertGrants access
 *
 * @remarks
 * Grant permissions to one or more users or roles. Safe to call multiple times — if a permission already exists it is left unchanged.
 */
export declare function useUpsertGrantsMutation(
  options?: MutationHookOptions<
    UpsertGrantsMutationData,
    UpsertGrantsMutationError,
    UpsertGrantsMutationVariables
  >,
): UseMutationResult<
  UpsertGrantsMutationData,
  UpsertGrantsMutationError,
  UpsertGrantsMutationVariables
>;
export declare function mutationKeyUpsertGrants(): MutationKey;
export declare function buildUpsertGrantsMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpsertGrantsMutationVariables,
  ) => Promise<UpsertGrantsMutationData>;
};
//# sourceMappingURL=upsertGrants.d.ts.map
