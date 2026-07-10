import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListAssistantsResult } from "../models/components/listassistantsresult.js";
import { ListAssistantsRequest, ListAssistantsSecurity } from "../models/operations/listassistants.js";
export type AssistantsListQueryData = ListAssistantsResult;
export declare function prefetchAssistantsList(queryClient: QueryClient, client$: GramCore, request?: ListAssistantsRequest | undefined, security?: ListAssistantsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildAssistantsListQuery(client$: GramCore, request?: ListAssistantsRequest | undefined, security?: ListAssistantsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<AssistantsListQueryData>;
};
export declare function queryKeyAssistantsList(parameters: {
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=assistantsList.core.d.ts.map