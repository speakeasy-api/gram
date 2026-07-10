import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListUserSessionConsentsRequest, ListUserSessionConsentsResponse, ListUserSessionConsentsSecurity } from "../models/operations/listusersessionconsents.js";
import { PageIterator } from "../types/operations.js";
export type UserSessionConsentsQueryData = ListUserSessionConsentsResponse;
export type UserSessionConsentsInfiniteQueryData = PageIterator<ListUserSessionConsentsResponse, {
    cursor: string;
}>;
export type UserSessionConsentsPageParams = PageIterator<ListUserSessionConsentsResponse, {
    cursor: string;
}>["~next"];
export declare function prefetchUserSessionConsents(queryClient: QueryClient, client$: GramCore, request?: ListUserSessionConsentsRequest | undefined, security?: ListUserSessionConsentsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function prefetchUserSessionConsentsInfinite(queryClient: QueryClient, client$: GramCore, request?: ListUserSessionConsentsRequest | undefined, security?: ListUserSessionConsentsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildUserSessionConsentsQuery(client$: GramCore, request?: ListUserSessionConsentsRequest | undefined, security?: ListUserSessionConsentsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<UserSessionConsentsQueryData>;
};
export declare function buildUserSessionConsentsInfiniteQuery(client$: GramCore, request?: ListUserSessionConsentsRequest | undefined, security?: ListUserSessionConsentsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext<QueryKey, UserSessionConsentsPageParams>) => Promise<UserSessionConsentsInfiniteQueryData>;
};
export declare function queryKeyUserSessionConsents(parameters: {
    subjectUrn?: string | undefined;
    userSessionClientId?: string | undefined;
    userSessionIssuerId?: string | undefined;
    cursor?: string | undefined;
    limit?: number | undefined;
    gramSession?: string | undefined;
    gramKey?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
export declare function queryKeyUserSessionConsentsInfinite(parameters: {
    subjectUrn?: string | undefined;
    userSessionClientId?: string | undefined;
    userSessionIssuerId?: string | undefined;
    cursor?: string | undefined;
    limit?: number | undefined;
    gramSession?: string | undefined;
    gramKey?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=userSessionConsents.core.d.ts.map