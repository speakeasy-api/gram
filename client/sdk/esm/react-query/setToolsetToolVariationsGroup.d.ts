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
  SetToolsetToolVariationsGroupRequest,
  SetToolsetToolVariationsGroupSecurity,
} from "../models/operations/settoolsettoolvariationsgroup.js";
import { MutationHookOptions } from "./_types.js";
export type SetToolsetToolVariationsGroupMutationVariables = {
  request: SetToolsetToolVariationsGroupRequest;
  security?: SetToolsetToolVariationsGroupSecurity | undefined;
  options?: RequestOptions;
};
export type SetToolsetToolVariationsGroupMutationData = Toolset;
export type SetToolsetToolVariationsGroupMutationError =
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
 * setToolVariationsGroup toolsets
 *
 * @remarks
 * Assign a tool variations group to a toolset to enable MCP tool filtering (or pass null to disable). The group must already exist in the caller's project.
 */
export declare function useSetToolsetToolVariationsGroupMutation(
  options?: MutationHookOptions<
    SetToolsetToolVariationsGroupMutationData,
    SetToolsetToolVariationsGroupMutationError,
    SetToolsetToolVariationsGroupMutationVariables
  >,
): UseMutationResult<
  SetToolsetToolVariationsGroupMutationData,
  SetToolsetToolVariationsGroupMutationError,
  SetToolsetToolVariationsGroupMutationVariables
>;
export declare function mutationKeySetToolsetToolVariationsGroup(): MutationKey;
export declare function buildSetToolsetToolVariationsGroupMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: SetToolsetToolVariationsGroupMutationVariables,
  ) => Promise<SetToolsetToolVariationsGroupMutationData>;
};
//# sourceMappingURL=setToolsetToolVariationsGroup.d.ts.map
