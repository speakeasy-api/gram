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
  SetProductFeatureRequest,
  SetProductFeatureSecurity,
} from "../models/operations/setproductfeature.js";
import { MutationHookOptions } from "./_types.js";
export type FeaturesSetMutationVariables = {
  request: SetProductFeatureRequest;
  security?: SetProductFeatureSecurity | undefined;
  options?: RequestOptions;
};
export type FeaturesSetMutationData = void;
export type FeaturesSetMutationError =
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
 * setProductFeature features
 *
 * @remarks
 * Enable or disable an organization feature flag.
 */
export declare function useFeaturesSetMutation(
  options?: MutationHookOptions<
    FeaturesSetMutationData,
    FeaturesSetMutationError,
    FeaturesSetMutationVariables
  >,
): UseMutationResult<
  FeaturesSetMutationData,
  FeaturesSetMutationError,
  FeaturesSetMutationVariables
>;
export declare function mutationKeyFeaturesSet(): MutationKey;
export declare function buildFeaturesSetMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: FeaturesSetMutationVariables,
  ) => Promise<FeaturesSetMutationData>;
};
//# sourceMappingURL=featuresSet.d.ts.map
