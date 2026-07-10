import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { PublishStatusResult } from "../models/components/publishstatusresult.js";
import { GetPublishStatusRequest, GetPublishStatusSecurity } from "../models/operations/getpublishstatus.js";
export type PublishStatusQueryData = PublishStatusResult;
export declare function prefetchPublishStatus(queryClient: QueryClient, client$: GramCore, request?: GetPublishStatusRequest | undefined, security?: GetPublishStatusSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildPublishStatusQuery(client$: GramCore, request?: GetPublishStatusRequest | undefined, security?: GetPublishStatusSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<PublishStatusQueryData>;
};
export declare function queryKeyPublishStatus(parameters: {
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=publishStatus.core.d.ts.map