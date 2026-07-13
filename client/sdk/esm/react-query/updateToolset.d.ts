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
  UpdateToolsetRequest,
  UpdateToolsetSecurity,
} from "../models/operations/updatetoolset.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateToolsetMutationVariables = {
  request: UpdateToolsetRequest;
  security?: UpdateToolsetSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateToolsetMutationData = Toolset;
export type UpdateToolsetMutationError =
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
 * updateToolset toolsets
 *
 * @remarks
 * Update a toolset's properties including name, description, and HTTP tools
 */
export declare function useUpdateToolsetMutation(
  options?: MutationHookOptions<
    UpdateToolsetMutationData,
    UpdateToolsetMutationError,
    UpdateToolsetMutationVariables
  >,
): UseMutationResult<
  UpdateToolsetMutationData,
  UpdateToolsetMutationError,
  UpdateToolsetMutationVariables
>;
export declare function mutationKeyUpdateToolset(): MutationKey;
export declare function buildUpdateToolsetMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateToolsetMutationVariables,
  ) => Promise<UpdateToolsetMutationData>;
};
//# sourceMappingURL=updateToolset.d.ts.map
