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
export type CreateSlackAppMutationVariables = {
    request: operations.CreateSlackAppRequest;
    security?: operations.CreateSlackAppSecurity | undefined;
    options?: RequestOptions;
};
export type CreateSlackAppMutationData = components.CreateSlackAppResult;
export type CreateSlackAppMutationError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createSlackApp slack
 *
 * @remarks
 * Create a new Slack app and generate its manifest.
 */
export declare function useCreateSlackAppMutation(options?: MutationHookOptions<CreateSlackAppMutationData, CreateSlackAppMutationError, CreateSlackAppMutationVariables>): UseMutationResult<CreateSlackAppMutationData, CreateSlackAppMutationError, CreateSlackAppMutationVariables>;
export declare function mutationKeyCreateSlackApp(): MutationKey;
export declare function buildCreateSlackAppMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CreateSlackAppMutationVariables) => Promise<CreateSlackAppMutationData>;
};
//# sourceMappingURL=createSlackApp.d.ts.map