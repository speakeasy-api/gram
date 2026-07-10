import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import {
  GetGlobalRemoteSessionClientRequest,
  GetGlobalRemoteSessionClientSecurity,
} from "../models/operations/getglobalremotesessionclient.js";
export type GlobalRemoteSessionClientQueryData = RemoteSessionClient;
export declare function prefetchGlobalRemoteSessionClient(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetGlobalRemoteSessionClientRequest,
  security?: GetGlobalRemoteSessionClientSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGlobalRemoteSessionClientQuery(
  client$: GramCore,
  request: GetGlobalRemoteSessionClientRequest,
  security?: GetGlobalRemoteSessionClientSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<GlobalRemoteSessionClientQueryData>;
};
export declare function queryKeyGlobalRemoteSessionClient(parameters: {
  id: string;
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=globalRemoteSessionClient.core.d.ts.map
