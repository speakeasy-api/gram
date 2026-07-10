import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListChatsResult } from "../models/components/listchatsresult.js";
import { AccountType, HasRisk, ListChatsRequest, ListChatsSecurity, Pinned, SortBy, SortOrder } from "../models/operations/listchats.js";
export type ListChatsQueryData = ListChatsResult;
export declare function prefetchListChats(queryClient: QueryClient, client$: GramCore, request?: ListChatsRequest | undefined, security?: ListChatsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListChatsQuery(client$: GramCore, request?: ListChatsRequest | undefined, security?: ListChatsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListChatsQueryData>;
};
export declare function queryKeyListChats(parameters: {
    search?: string | undefined;
    externalUserId?: string | undefined;
    source?: string | undefined;
    assistantId?: string | undefined;
    sourceKind?: string | undefined;
    excludeSourceKind?: string | undefined;
    hasRisk?: HasRisk | undefined;
    accountType?: AccountType | undefined;
    pinned?: Pinned | undefined;
    minRiskScore?: number | undefined;
    from?: Date | undefined;
    to?: Date | undefined;
    limit?: number | undefined;
    offset?: number | undefined;
    sortBy?: SortBy | undefined;
    sortOrder?: SortOrder | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
    gramChatSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listChats.core.d.ts.map