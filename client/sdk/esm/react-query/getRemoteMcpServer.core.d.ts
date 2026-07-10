import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteMcpServer } from "../models/components/remotemcpserver.js";
import { GetRemoteMcpServerRequest, GetRemoteMcpServerSecurity } from "../models/operations/getremotemcpserver.js";
export type GetRemoteMcpServerQueryData = RemoteMcpServer;
export declare function prefetchGetRemoteMcpServer(queryClient: QueryClient, client$: GramCore, request?: GetRemoteMcpServerRequest | undefined, security?: GetRemoteMcpServerSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildGetRemoteMcpServerQuery(client$: GramCore, request?: GetRemoteMcpServerRequest | undefined, security?: GetRemoteMcpServerSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<GetRemoteMcpServerQueryData>;
};
export declare function queryKeyGetRemoteMcpServer(parameters: {
    id?: string | undefined;
    slug?: string | undefined;
    gramSession?: string | undefined;
    gramKey?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getRemoteMcpServer.core.d.ts.map