import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Assistant } from "../models/components/assistant.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateAssistantRequest, UpdateAssistantSecurity } from "../models/operations/updateassistant.js";
import { MutationHookOptions } from "./_types.js";
export type AssistantsUpdateMutationVariables = {
    request: UpdateAssistantRequest;
    security?: UpdateAssistantSecurity | undefined;
    options?: RequestOptions;
};
export type AssistantsUpdateMutationData = Assistant;
export type AssistantsUpdateMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * updateAssistant assistants
 *
 * @remarks
 * Update an assistant.
 */
export declare function useAssistantsUpdateMutation(options?: MutationHookOptions<AssistantsUpdateMutationData, AssistantsUpdateMutationError, AssistantsUpdateMutationVariables>): UseMutationResult<AssistantsUpdateMutationData, AssistantsUpdateMutationError, AssistantsUpdateMutationVariables>;
export declare function mutationKeyAssistantsUpdate(): MutationKey;
export declare function buildAssistantsUpdateMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: AssistantsUpdateMutationVariables) => Promise<AssistantsUpdateMutationData>;
};
//# sourceMappingURL=assistantsUpdate.d.ts.map