import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GenerateTitleResponseBody } from "../models/components/generatetitleresponsebody.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GenerateTitleRequest, GenerateTitleSecurity } from "../models/operations/generatetitle.js";
import { MutationHookOptions } from "./_types.js";
export type ChatGenerateTitleMutationVariables = {
    request: GenerateTitleRequest;
    security?: GenerateTitleSecurity | undefined;
    options?: RequestOptions;
};
export type ChatGenerateTitleMutationData = GenerateTitleResponseBody;
export type ChatGenerateTitleMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * generateTitle chat
 *
 * @remarks
 * Read or set a chat's title. Omit `title` to return the current/auto-generated title (titles are generated asynchronously after a completion). Provide `title` to set a manual title that auto-generation will never overwrite; provide an empty `title` to clear the manual title and re-enable auto-generation.
 */
export declare function useChatGenerateTitleMutation(options?: MutationHookOptions<ChatGenerateTitleMutationData, ChatGenerateTitleMutationError, ChatGenerateTitleMutationVariables>): UseMutationResult<ChatGenerateTitleMutationData, ChatGenerateTitleMutationError, ChatGenerateTitleMutationVariables>;
export declare function mutationKeyChatGenerateTitle(): MutationKey;
export declare function buildChatGenerateTitleMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: ChatGenerateTitleMutationVariables) => Promise<ChatGenerateTitleMutationData>;
};
//# sourceMappingURL=chatGenerateTitle.d.ts.map