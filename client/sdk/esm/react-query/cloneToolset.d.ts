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
  CloneToolsetRequest,
  CloneToolsetSecurity,
} from "../models/operations/clonetoolset.js";
import { MutationHookOptions } from "./_types.js";
export type CloneToolsetMutationVariables = {
  request: CloneToolsetRequest;
  security?: CloneToolsetSecurity | undefined;
  options?: RequestOptions;
};
export type CloneToolsetMutationData = Toolset;
export type CloneToolsetMutationError =
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
 * cloneToolset toolsets
 *
 * @remarks
 * Clone an existing toolset with a new name
 */
export declare function useCloneToolsetMutation(
  options?: MutationHookOptions<
    CloneToolsetMutationData,
    CloneToolsetMutationError,
    CloneToolsetMutationVariables
  >,
): UseMutationResult<
  CloneToolsetMutationData,
  CloneToolsetMutationError,
  CloneToolsetMutationVariables
>;
export declare function mutationKeyCloneToolset(): MutationKey;
export declare function buildCloneToolsetMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CloneToolsetMutationVariables,
  ) => Promise<CloneToolsetMutationData>;
};
//# sourceMappingURL=cloneToolset.d.ts.map
