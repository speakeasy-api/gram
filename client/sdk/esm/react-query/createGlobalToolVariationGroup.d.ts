import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ToolVariationGroupResult } from "../models/components/toolvariationgroupresult.js";
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
  CreateGlobalToolVariationGroupRequest,
  CreateGlobalToolVariationGroupSecurity,
} from "../models/operations/createglobaltoolvariationgroup.js";
import { MutationHookOptions } from "./_types.js";
export type CreateGlobalToolVariationGroupMutationVariables = {
  request?: CreateGlobalToolVariationGroupRequest | undefined;
  security?: CreateGlobalToolVariationGroupSecurity | undefined;
  options?: RequestOptions;
};
export type CreateGlobalToolVariationGroupMutationData =
  ToolVariationGroupResult;
export type CreateGlobalToolVariationGroupMutationError =
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
 * createGlobal variations
 *
 * @remarks
 * Ensure the project-default (global) tool variation group exists, returning it. Idempotent: returns the existing group unchanged when present, otherwise creates it. Takes no parameters and only manages the single project-default group.
 */
export declare function useCreateGlobalToolVariationGroupMutation(
  options?: MutationHookOptions<
    CreateGlobalToolVariationGroupMutationData,
    CreateGlobalToolVariationGroupMutationError,
    CreateGlobalToolVariationGroupMutationVariables
  >,
): UseMutationResult<
  CreateGlobalToolVariationGroupMutationData,
  CreateGlobalToolVariationGroupMutationError,
  CreateGlobalToolVariationGroupMutationVariables
>;
export declare function mutationKeyCreateGlobalToolVariationGroup(): MutationKey;
export declare function buildCreateGlobalToolVariationGroupMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateGlobalToolVariationGroupMutationVariables,
  ) => Promise<CreateGlobalToolVariationGroupMutationData>;
};
//# sourceMappingURL=createGlobalToolVariationGroup.d.ts.map
