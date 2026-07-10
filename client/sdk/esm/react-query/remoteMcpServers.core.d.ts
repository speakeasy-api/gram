import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListServersResult } from "../models/components/listserversresult.js";
import { ListRemoteMcpServersRequest, ListRemoteMcpServersSecurity } from "../models/operations/listremotemcpservers.js";
export type RemoteMcpServersQueryData = ListServersResult;
export declare function prefetchRemoteMcpServers(queryClient: QueryClient, client$: GramCore, request?: ListRemoteMcpServersRequest | undefined, security?: ListRemoteMcpServersSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRemoteMcpServersQuery(client$: GramCore, request?: ListRemoteMcpServersRequest | undefined, security?: ListRemoteMcpServersSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RemoteMcpServersQueryData>;
};
export declare function queryKeyRemoteMcpServers(parameters: {
    gramSession?: string | undefined;
    gramKey?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=remoteMcpServers.core.d.ts.map