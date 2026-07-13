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
  CreateToolsetRequest,
  CreateToolsetSecurity,
} from "../models/operations/createtoolset.js";
import { MutationHookOptions } from "./_types.js";
export type CreateToolsetMutationVariables = {
  request: CreateToolsetRequest;
  security?: CreateToolsetSecurity | undefined;
  options?: RequestOptions;
};
export type CreateToolsetMutationData = Toolset;
export type CreateToolsetMutationError =
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
 * createToolset toolsets
 *
 * @remarks
 * Create a new toolset with associated tools
 */
export declare function useCreateToolsetMutation(
  options?: MutationHookOptions<
    CreateToolsetMutationData,
    CreateToolsetMutationError,
    CreateToolsetMutationVariables
  >,
): UseMutationResult<
  CreateToolsetMutationData,
  CreateToolsetMutationError,
  CreateToolsetMutationVariables
>;
export declare function mutationKeyCreateToolset(): MutationKey;
export declare function buildCreateToolsetMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateToolsetMutationVariables,
  ) => Promise<CreateToolsetMutationData>;
};
//# sourceMappingURL=createToolset.d.ts.map
