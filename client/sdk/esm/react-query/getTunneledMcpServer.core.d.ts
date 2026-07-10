import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TunneledMcpServer } from "../models/components/tunneledmcpserver.js";
import {
  GetTunneledMcpServerRequest,
  GetTunneledMcpServerSecurity,
} from "../models/operations/gettunneledmcpserver.js";
export type GetTunneledMcpServerQueryData = TunneledMcpServer;
export declare function prefetchGetTunneledMcpServer(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetTunneledMcpServerRequest,
  security?: GetTunneledMcpServerSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetTunneledMcpServerQuery(
  client$: GramCore,
  request: GetTunneledMcpServerRequest,
  security?: GetTunneledMcpServerSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<GetTunneledMcpServerQueryData>;
};
export declare function queryKeyGetTunneledMcpServer(parameters: {
  id: string;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getTunneledMcpServer.core.d.ts.map
