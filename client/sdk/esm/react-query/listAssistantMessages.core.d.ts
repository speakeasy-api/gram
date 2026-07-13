import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type ListAssistantMessagesQueryData = components.ListMessagesResult;
export declare function prefetchListAssistantMessages(queryClient: QueryClient, client$: GramCore, request: operations.ListAssistantMessagesRequest, security?: operations.ListAssistantMessagesSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListAssistantMessagesQuery(client$: GramCore, request: operations.ListAssistantMessagesRequest, security?: operations.ListAssistantMessagesSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListAssistantMessagesQueryData>;
};
export declare function queryKeyListAssistantMessages(parameters: {
    chatId: string;
    afterSeq?: number | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listAssistantMessages.core.d.ts.map