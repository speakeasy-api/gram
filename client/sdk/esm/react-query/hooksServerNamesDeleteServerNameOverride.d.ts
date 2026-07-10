import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteServerNameOverrideRequest, DeleteServerNameOverrideSecurity } from "../models/operations/deleteservernameoverride.js";
import { MutationHookOptions } from "./_types.js";
export type HooksServerNamesDeleteServerNameOverrideMutationVariables = {
    request: DeleteServerNameOverrideRequest;
    security?: DeleteServerNameOverrideSecurity | undefined;
    options?: RequestOptions;
};
export type HooksServerNamesDeleteServerNameOverrideMutationData = void;
export type HooksServerNamesDeleteServerNameOverrideMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * delete hooksServerNames
 *
 * @remarks
 * Delete a server name display override
 */
export declare function useHooksServerNamesDeleteServerNameOverrideMutation(options?: MutationHookOptions<HooksServerNamesDeleteServerNameOverrideMutationData, HooksServerNamesDeleteServerNameOverrideMutationError, HooksServerNamesDeleteServerNameOverrideMutationVariables>): UseMutationResult<HooksServerNamesDeleteServerNameOverrideMutationData, HooksServerNamesDeleteServerNameOverrideMutationError, HooksServerNamesDeleteServerNameOverrideMutationVariables>;
export declare function mutationKeyHooksServerNamesDeleteServerNameOverride(): MutationKey;
export declare function buildHooksServerNamesDeleteServerNameOverrideMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: HooksServerNamesDeleteServerNameOverrideMutationVariables) => Promise<HooksServerNamesDeleteServerNameOverrideMutationData>;
};
//# sourceMappingURL=hooksServerNamesDeleteServerNameOverride.d.ts.map