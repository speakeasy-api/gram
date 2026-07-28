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
  EnableWebhooksRequest,
  EnableWebhooksSecurity,
} from "../models/operations/enablewebhooks.js";
import { MutationHookOptions } from "./_types.js";
export type EnableWebhooksMutationVariables = {
  request?: EnableWebhooksRequest | undefined;
  security?: EnableWebhooksSecurity | undefined;
  options?: RequestOptions;
};
export type EnableWebhooksMutationData = void;
export type EnableWebhooksMutationError =
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
 * enableWebhooks organizations
 *
 * @remarks
 * Enable  webhooks for the active organization.
 */
export declare function useEnableWebhooksMutation(
  options?: MutationHookOptions<
    EnableWebhooksMutationData,
    EnableWebhooksMutationError,
    EnableWebhooksMutationVariables
  >,
): UseMutationResult<
  EnableWebhooksMutationData,
  EnableWebhooksMutationError,
  EnableWebhooksMutationVariables
>;
export declare function mutationKeyEnableWebhooks(): MutationKey;
export declare function buildEnableWebhooksMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: EnableWebhooksMutationVariables,
  ) => Promise<EnableWebhooksMutationData>;
};
//# sourceMappingURL=enableWebhooks.d.ts.map
