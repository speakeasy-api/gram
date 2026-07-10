import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListAssistantsRequest, ListAssistantsSecurity } from "../models/operations/listassistants.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { AssistantsListQueryData, buildAssistantsListQuery, prefetchAssistantsList, queryKeyAssistantsList } from "./assistantsList.core.js";
export { type AssistantsListQueryData, buildAssistantsListQuery, prefetchAssistantsList, queryKeyAssistantsList, };
export type AssistantsListQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listAssistants assistants
 *
 * @remarks
 * List assistants for the current project.
 */
export declare function useAssistantsList(request?: ListAssistantsRequest | undefined, security?: ListAssistantsSecurity | undefined, options?: QueryHookOptions<AssistantsListQueryData, AssistantsListQueryError>): UseQueryResult<AssistantsListQueryData, AssistantsListQueryError>;
/**
 * listAssistants assistants
 *
 * @remarks
 * List assistants for the current project.
 */
export declare function useAssistantsListSuspense(request?: ListAssistantsRequest | undefined, security?: ListAssistantsSecurity | undefined, options?: SuspenseQueryHookOptions<AssistantsListQueryData, AssistantsListQueryError>): UseSuspenseQueryResult<AssistantsListQueryData, AssistantsListQueryError>;
export declare function setAssistantsListData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: AssistantsListQueryData): AssistantsListQueryData | undefined;
export declare function invalidateAssistantsList(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllAssistantsList(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=assistantsList.d.ts.map