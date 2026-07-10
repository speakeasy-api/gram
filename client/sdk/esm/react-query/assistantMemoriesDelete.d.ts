import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteAssistantMemoryRequest, DeleteAssistantMemorySecurity } from "../models/operations/deleteassistantmemory.js";
import { MutationHookOptions } from "./_types.js";
export type AssistantMemoriesDeleteMutationVariables = {
    request: DeleteAssistantMemoryRequest;
    security?: DeleteAssistantMemorySecurity | undefined;
    options?: RequestOptions;
};
export type AssistantMemoriesDeleteMutationData = void;
export type AssistantMemoriesDeleteMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteAssistantMemory assistantMemories
 *
 * @remarks
 * Delete an assistant memory by ID.
 */
export declare function useAssistantMemoriesDeleteMutation(options?: MutationHookOptions<AssistantMemoriesDeleteMutationData, AssistantMemoriesDeleteMutationError, AssistantMemoriesDeleteMutationVariables>): UseMutationResult<AssistantMemoriesDeleteMutationData, AssistantMemoriesDeleteMutationError, AssistantMemoriesDeleteMutationVariables>;
export declare function mutationKeyAssistantMemoriesDelete(): MutationKey;
export declare function buildAssistantMemoriesDeleteMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: AssistantMemoriesDeleteMutationVariables) => Promise<AssistantMemoriesDeleteMutationData>;
};
//# sourceMappingURL=assistantMemoriesDelete.d.ts.map