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
  HooksNumberMetricsRequest,
  HooksNumberMetricsSecurity,
} from "../models/operations/hooksnumbermetrics.js";
import { MutationHookOptions } from "./_types.js";
export type HooksHooksNumberMetricsMutationVariables = {
  request: HooksNumberMetricsRequest;
  security?: HooksNumberMetricsSecurity | undefined;
  options?: RequestOptions;
};
export type HooksHooksNumberMetricsMutationData = void;
export type HooksHooksNumberMetricsMutationError =
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
 * metrics hooks
 *
 * @remarks
 * Endpoint to receive OTEL metrics data from Claude Code. Requires API key authentication.
 */
export declare function useHooksHooksNumberMetricsMutation(
  options?: MutationHookOptions<
    HooksHooksNumberMetricsMutationData,
    HooksHooksNumberMetricsMutationError,
    HooksHooksNumberMetricsMutationVariables
  >,
): UseMutationResult<
  HooksHooksNumberMetricsMutationData,
  HooksHooksNumberMetricsMutationError,
  HooksHooksNumberMetricsMutationVariables
>;
export declare function mutationKeyHooksHooksNumberMetrics(): MutationKey;
export declare function buildHooksHooksNumberMetricsMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: HooksHooksNumberMetricsMutationVariables,
  ) => Promise<HooksHooksNumberMetricsMutationData>;
};
//# sourceMappingURL=hooksHooksNumberMetrics.d.ts.map
