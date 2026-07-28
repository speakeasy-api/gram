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
  DeleteOtelForwardingConfigRequest,
  DeleteOtelForwardingConfigSecurity,
} from "../models/operations/deleteotelforwardingconfig.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteOtelForwardingConfigMutationVariables = {
  request?: DeleteOtelForwardingConfigRequest | undefined;
  security?: DeleteOtelForwardingConfigSecurity | undefined;
  options?: RequestOptions;
};
export type DeleteOtelForwardingConfigMutationData = void;
export type DeleteOtelForwardingConfigMutationError =
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
 * deleteConfig otelForwarding
 *
 * @remarks
 * Delete the org-wide OTEL forwarding config.
 */
export declare function useDeleteOtelForwardingConfigMutation(
  options?: MutationHookOptions<
    DeleteOtelForwardingConfigMutationData,
    DeleteOtelForwardingConfigMutationError,
    DeleteOtelForwardingConfigMutationVariables
  >,
): UseMutationResult<
  DeleteOtelForwardingConfigMutationData,
  DeleteOtelForwardingConfigMutationError,
  DeleteOtelForwardingConfigMutationVariables
>;
export declare function mutationKeyDeleteOtelForwardingConfig(): MutationKey;
export declare function buildDeleteOtelForwardingConfigMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: DeleteOtelForwardingConfigMutationVariables,
  ) => Promise<DeleteOtelForwardingConfigMutationData>;
};
//# sourceMappingURL=deleteOtelForwardingConfig.d.ts.map
