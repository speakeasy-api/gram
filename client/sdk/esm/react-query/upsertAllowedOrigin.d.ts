import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UpsertAllowedOriginResult } from "../models/components/upsertallowedoriginresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpsertAllowedOriginRequest, UpsertAllowedOriginSecurity } from "../models/operations/upsertallowedorigin.js";
import { MutationHookOptions } from "./_types.js";
export type UpsertAllowedOriginMutationVariables = {
    request: UpsertAllowedOriginRequest;
    security?: UpsertAllowedOriginSecurity | undefined;
    options?: RequestOptions;
};
export type UpsertAllowedOriginMutationData = UpsertAllowedOriginResult;
export type UpsertAllowedOriginMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * upsertAllowedOrigin projects
 *
 * @remarks
 * Upsert an allowed origin for a project.
 */
export declare function useUpsertAllowedOriginMutation(options?: MutationHookOptions<UpsertAllowedOriginMutationData, UpsertAllowedOriginMutationError, UpsertAllowedOriginMutationVariables>): UseMutationResult<UpsertAllowedOriginMutationData, UpsertAllowedOriginMutationError, UpsertAllowedOriginMutationVariables>;
export declare function mutationKeyUpsertAllowedOrigin(): MutationKey;
export declare function buildUpsertAllowedOriginMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: UpsertAllowedOriginMutationVariables) => Promise<UpsertAllowedOriginMutationData>;
};
//# sourceMappingURL=upsertAllowedOrigin.d.ts.map