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
export type ConfigureSlackAppMutationVariables = {
    request: operations.ConfigureSlackAppRequest;
    security?: operations.ConfigureSlackAppSecurity | undefined;
    options?: RequestOptions;
};
export type ConfigureSlackAppMutationData = components.SlackAppResult;
export type ConfigureSlackAppMutationError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * configureSlackApp slack
 *
 * @remarks
 * Store Slack credentials (client ID, client secret, signing secret) for an app.
 */
export declare function useConfigureSlackAppMutation(options?: MutationHookOptions<ConfigureSlackAppMutationData, ConfigureSlackAppMutationError, ConfigureSlackAppMutationVariables>): UseMutationResult<ConfigureSlackAppMutationData, ConfigureSlackAppMutationError, ConfigureSlackAppMutationVariables>;
export declare function mutationKeyConfigureSlackApp(): MutationKey;
export declare function buildConfigureSlackAppMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: ConfigureSlackAppMutationVariables) => Promise<ConfigureSlackAppMutationData>;
};
//# sourceMappingURL=configureSlackApp.d.ts.map