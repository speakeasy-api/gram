import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ServeChatAttachmentSignedRequest, ServeChatAttachmentSignedResponse } from "../models/operations/servechatattachmentsigned.js";
export type ServeChatAttachmentSignedQueryData = ServeChatAttachmentSignedResponse;
export declare function prefetchServeChatAttachmentSigned(queryClient: QueryClient, client$: GramCore, request: ServeChatAttachmentSignedRequest, options?: RequestOptions): Promise<void>;
export declare function buildServeChatAttachmentSignedQuery(client$: GramCore, request: ServeChatAttachmentSignedRequest, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ServeChatAttachmentSignedQueryData>;
};
export declare function queryKeyServeChatAttachmentSigned(parameters: {
    token: string;
}): QueryKey;
//# sourceMappingURL=serveChatAttachmentSigned.core.d.ts.map