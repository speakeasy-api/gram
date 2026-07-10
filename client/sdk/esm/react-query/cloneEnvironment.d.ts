import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Environment } from "../models/components/environment.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CloneEnvironmentRequest, CloneEnvironmentSecurity } from "../models/operations/cloneenvironment.js";
import { MutationHookOptions } from "./_types.js";
export type CloneEnvironmentMutationVariables = {
    request: CloneEnvironmentRequest;
    security?: CloneEnvironmentSecurity | undefined;
    options?: RequestOptions;
};
export type CloneEnvironmentMutationData = Environment;
export type CloneEnvironmentMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * cloneEnvironment environments
 *
 * @remarks
 * Clone an environment into a new one. Either copies only the variable names with empty placeholder values, or copies the encrypted values verbatim. Encrypted secret values are never decrypted by the application during the clone operation.
 */
export declare function useCloneEnvironmentMutation(options?: MutationHookOptions<CloneEnvironmentMutationData, CloneEnvironmentMutationError, CloneEnvironmentMutationVariables>): UseMutationResult<CloneEnvironmentMutationData, CloneEnvironmentMutationError, CloneEnvironmentMutationVariables>;
export declare function mutationKeyCloneEnvironment(): MutationKey;
export declare function buildCloneEnvironmentMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CloneEnvironmentMutationVariables) => Promise<CloneEnvironmentMutationData>;
};
//# sourceMappingURL=cloneEnvironment.d.ts.map