import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UpsertGlobalToolVariationResult } from "../models/components/upsertglobaltoolvariationresult.js";
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
  UpsertGlobalVariationRequest,
  UpsertGlobalVariationSecurity,
} from "../models/operations/upsertglobalvariation.js";
import { MutationHookOptions } from "./_types.js";
export type UpsertGlobalVariationMutationVariables = {
  request: UpsertGlobalVariationRequest;
  security?: UpsertGlobalVariationSecurity | undefined;
  options?: RequestOptions;
};
export type UpsertGlobalVariationMutationData = UpsertGlobalToolVariationResult;
export type UpsertGlobalVariationMutationError =
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
 * upsertGlobal variations
 *
 * @remarks
 * Create or update a globally defined tool variation.
 */
export declare function useUpsertGlobalVariationMutation(
  options?: MutationHookOptions<
    UpsertGlobalVariationMutationData,
    UpsertGlobalVariationMutationError,
    UpsertGlobalVariationMutationVariables
  >,
): UseMutationResult<
  UpsertGlobalVariationMutationData,
  UpsertGlobalVariationMutationError,
  UpsertGlobalVariationMutationVariables
>;
export declare function mutationKeyUpsertGlobalVariation(): MutationKey;
export declare function buildUpsertGlobalVariationMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpsertGlobalVariationMutationVariables,
  ) => Promise<UpsertGlobalVariationMutationData>;
};
//# sourceMappingURL=upsertGlobalVariation.d.ts.map
