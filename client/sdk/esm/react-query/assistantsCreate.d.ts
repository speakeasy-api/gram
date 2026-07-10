import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Assistant } from "../models/components/assistant.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateAssistantRequest, CreateAssistantSecurity } from "../models/operations/createassistant.js";
import { MutationHookOptions } from "./_types.js";
export type AssistantsCreateMutationVariables = {
    request: CreateAssistantRequest;
    security?: CreateAssistantSecurity | undefined;
    options?: RequestOptions;
};
export type AssistantsCreateMutationData = Assistant;
export type AssistantsCreateMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createAssistant assistants
 *
 * @remarks
 * Create an assistant.
 */
export declare function useAssistantsCreateMutation(options?: MutationHookOptions<AssistantsCreateMutationData, AssistantsCreateMutationError, AssistantsCreateMutationVariables>): UseMutationResult<AssistantsCreateMutationData, AssistantsCreateMutationError, AssistantsCreateMutationVariables>;
export declare function mutationKeyAssistantsCreate(): MutationKey;
export declare function buildAssistantsCreateMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: AssistantsCreateMutationVariables) => Promise<AssistantsCreateMutationData>;
};
//# sourceMappingURL=assistantsCreate.d.ts.map