import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListTunneledMcpServersResult } from "../models/components/listtunneledmcpserversresult.js";
import { ListTunneledMcpServersRequest, ListTunneledMcpServersSecurity } from "../models/operations/listtunneledmcpservers.js";
export type TunneledMcpServersQueryData = ListTunneledMcpServersResult;
export declare function prefetchTunneledMcpServers(queryClient: QueryClient, client$: GramCore, request?: ListTunneledMcpServersRequest | undefined, security?: ListTunneledMcpServersSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildTunneledMcpServersQuery(client$: GramCore, request?: ListTunneledMcpServersRequest | undefined, security?: ListTunneledMcpServersSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<TunneledMcpServersQueryData>;
};
export declare function queryKeyTunneledMcpServers(parameters: {
    gramSession?: string | undefined;
    gramKey?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=tunneledMcpServers.core.d.ts.map