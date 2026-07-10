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
  HooksNumberLogsRequest,
  HooksNumberLogsSecurity,
} from "../models/operations/hooksnumberlogs.js";
import { MutationHookOptions } from "./_types.js";
export type HooksHooksNumberLogsMutationVariables = {
  request: HooksNumberLogsRequest;
  security?: HooksNumberLogsSecurity | undefined;
  options?: RequestOptions;
};
export type HooksHooksNumberLogsMutationData = void;
export type HooksHooksNumberLogsMutationError =
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
 * logs hooks
 *
 * @remarks
 * Endpoint to receive OTEL logs data from Claude Code. Requires API key authentication.
 */
export declare function useHooksHooksNumberLogsMutation(
  options?: MutationHookOptions<
    HooksHooksNumberLogsMutationData,
    HooksHooksNumberLogsMutationError,
    HooksHooksNumberLogsMutationVariables
  >,
): UseMutationResult<
  HooksHooksNumberLogsMutationData,
  HooksHooksNumberLogsMutationError,
  HooksHooksNumberLogsMutationVariables
>;
export declare function mutationKeyHooksHooksNumberLogs(): MutationKey;
export declare function buildHooksHooksNumberLogsMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: HooksHooksNumberLogsMutationVariables,
  ) => Promise<HooksHooksNumberLogsMutationData>;
};
//# sourceMappingURL=hooksHooksNumberLogs.d.ts.map
