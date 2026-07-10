import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TunneledMcpServerConnections } from "../models/components/tunneledmcpserverconnections.js";
import { ListTunneledMcpServerConnectionsRequest, ListTunneledMcpServerConnectionsSecurity } from "../models/operations/listtunneledmcpserverconnections.js";
export type ListTunneledMcpServerConnectionsQueryData = TunneledMcpServerConnections;
export declare function prefetchListTunneledMcpServerConnections(queryClient: QueryClient, client$: GramCore, request: ListTunneledMcpServerConnectionsRequest, security?: ListTunneledMcpServerConnectionsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListTunneledMcpServerConnectionsQuery(client$: GramCore, request: ListTunneledMcpServerConnectionsRequest, security?: ListTunneledMcpServerConnectionsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListTunneledMcpServerConnectionsQueryData>;
};
export declare function queryKeyListTunneledMcpServerConnections(parameters: {
    id: string;
    gramSession?: string | undefined;
    gramKey?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listTunneledMcpServerConnections.core.d.ts.map