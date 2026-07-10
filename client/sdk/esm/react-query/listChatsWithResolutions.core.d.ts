import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type ListChatsWithResolutionsQueryData = components.ListChatsWithResolutionsResult;
export declare function prefetchListChatsWithResolutions(queryClient: QueryClient, client$: GramCore, request?: operations.ListChatsWithResolutionsRequest | undefined, security?: operations.ListChatsWithResolutionsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListChatsWithResolutionsQuery(client$: GramCore, request?: operations.ListChatsWithResolutionsRequest | undefined, security?: operations.ListChatsWithResolutionsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListChatsWithResolutionsQueryData>;
};
export declare function queryKeyListChatsWithResolutions(parameters: {
    search?: string | undefined;
    externalUserId?: string | undefined;
    assistantId?: string | undefined;
    resolutionStatus?: string | undefined;
    hasRisk?: operations.HasRisk | undefined;
    from?: Date | undefined;
    to?: Date | undefined;
    limit?: number | undefined;
    offset?: number | undefined;
    sortBy?: operations.SortBy | undefined;
    sortOrder?: operations.SortOrder | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
    gramChatSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listChatsWithResolutions.core.d.ts.map