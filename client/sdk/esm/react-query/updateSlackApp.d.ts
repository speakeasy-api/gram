import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateSlackAppMutationVariables = {
    request: operations.UpdateSlackAppRequest;
    security?: operations.UpdateSlackAppSecurity | undefined;
    options?: RequestOptions;
};
export type UpdateSlackAppMutationData = components.SlackAppResult;
export type UpdateSlackAppMutationError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * updateSlackApp slack
 *
 * @remarks
 * Update a Slack app's settings.
 */
export declare function useUpdateSlackAppMutation(options?: MutationHookOptions<UpdateSlackAppMutationData, UpdateSlackAppMutationError, UpdateSlackAppMutationVariables>): UseMutationResult<UpdateSlackAppMutationData, UpdateSlackAppMutationError, UpdateSlackAppMutationVariables>;
export declare function mutationKeyUpdateSlackApp(): MutationKey;
export declare function buildUpdateSlackAppMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: UpdateSlackAppMutationVariables) => Promise<UpdateSlackAppMutationData>;
};
//# sourceMappingURL=updateSlackApp.d.ts.map