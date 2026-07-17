import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { OtelForwardingConfig } from "../models/components/otelforwardingconfig.js";
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
  UpsertOtelForwardingConfigRequest,
  UpsertOtelForwardingConfigSecurity,
} from "../models/operations/upsertotelforwardingconfig.js";
import { MutationHookOptions } from "./_types.js";
export type UpsertOtelForwardingConfigMutationVariables = {
  request: UpsertOtelForwardingConfigRequest;
  security?: UpsertOtelForwardingConfigSecurity | undefined;
  options?: RequestOptions;
};
export type UpsertOtelForwardingConfigMutationData = OtelForwardingConfig;
export type UpsertOtelForwardingConfigMutationError =
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
 * upsertConfig otelForwarding
 *
 * @remarks
 * Create or update the org-wide OTEL forwarding config. Replaces the full header set on each call.
 */
export declare function useUpsertOtelForwardingConfigMutation(
  options?: MutationHookOptions<
    UpsertOtelForwardingConfigMutationData,
    UpsertOtelForwardingConfigMutationError,
    UpsertOtelForwardingConfigMutationVariables
  >,
): UseMutationResult<
  UpsertOtelForwardingConfigMutationData,
  UpsertOtelForwardingConfigMutationError,
  UpsertOtelForwardingConfigMutationVariables
>;
export declare function mutationKeyUpsertOtelForwardingConfig(): MutationKey;
export declare function buildUpsertOtelForwardingConfigMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpsertOtelForwardingConfigMutationVariables,
  ) => Promise<UpsertOtelForwardingConfigMutationData>;
};
//# sourceMappingURL=upsertOtelForwardingConfig.d.ts.map
