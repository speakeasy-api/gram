import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DisableWebhooksRequest, DisableWebhooksSecurity } from "../models/operations/disablewebhooks.js";
import { MutationHookOptions } from "./_types.js";
export type DisableWebhooksMutationVariables = {
    request?: DisableWebhooksRequest | undefined;
    security?: DisableWebhooksSecurity | undefined;
    options?: RequestOptions;
};
export type DisableWebhooksMutationData = void;
export type DisableWebhooksMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * disableWebhooks organizations
 *
 * @remarks
 * Disable  webhooks for the active organization.
 */
export declare function useDisableWebhooksMutation(options?: MutationHookOptions<DisableWebhooksMutationData, DisableWebhooksMutationError, DisableWebhooksMutationVariables>): UseMutationResult<DisableWebhooksMutationData, DisableWebhooksMutationError, DisableWebhooksMutationVariables>;
export declare function mutationKeyDisableWebhooks(): MutationKey;
export declare function buildDisableWebhooksMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DisableWebhooksMutationVariables) => Promise<DisableWebhooksMutationData>;
};
//# sourceMappingURL=disableWebhooks.d.ts.map