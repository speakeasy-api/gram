import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListAssistantMessagesQuery, ListAssistantMessagesQueryData, prefetchListAssistantMessages, queryKeyListAssistantMessages } from "./listAssistantMessages.core.js";
export { buildListAssistantMessagesQuery, type ListAssistantMessagesQueryData, prefetchListAssistantMessages, queryKeyListAssistantMessages, };
export type ListAssistantMessagesQueryError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listMessages assistants
 *
 * @remarks
 * List a dashboard conversation log for a chat (the user's messages and the assistant's delivered replies). Only the user who owns the conversation may read it. Poll with after_seq to fetch only newer messages.
 */
export declare function useListAssistantMessages(request: operations.ListAssistantMessagesRequest, security?: operations.ListAssistantMessagesSecurity | undefined, options?: QueryHookOptions<ListAssistantMessagesQueryData, ListAssistantMessagesQueryError>): UseQueryResult<ListAssistantMessagesQueryData, ListAssistantMessagesQueryError>;
/**
 * listMessages assistants
 *
 * @remarks
 * List a dashboard conversation log for a chat (the user's messages and the assistant's delivered replies). Only the user who owns the conversation may read it. Poll with after_seq to fetch only newer messages.
 */
export declare function useListAssistantMessagesSuspense(request: operations.ListAssistantMessagesRequest, security?: operations.ListAssistantMessagesSecurity | undefined, options?: SuspenseQueryHookOptions<ListAssistantMessagesQueryData, ListAssistantMessagesQueryError>): UseSuspenseQueryResult<ListAssistantMessagesQueryData, ListAssistantMessagesQueryError>;
export declare function setListAssistantMessagesData(client: QueryClient, queryKeyBase: [
    parameters: {
        chatId: string;
        afterSeq?: number | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: ListAssistantMessagesQueryData): ListAssistantMessagesQueryData | undefined;
export declare function invalidateListAssistantMessages(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        chatId: string;
        afterSeq?: number | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListAssistantMessages(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listAssistantMessages.d.ts.map