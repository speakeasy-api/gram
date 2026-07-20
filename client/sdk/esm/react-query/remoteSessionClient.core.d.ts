import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import {
  GetRemoteSessionClientRequest,
  GetRemoteSessionClientSecurity,
} from "../models/operations/getremotesessionclient.js";
export type RemoteSessionClientQueryData = RemoteSessionClient;
export declare function prefetchRemoteSessionClient(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetRemoteSessionClientRequest,
  security?: GetRemoteSessionClientSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildRemoteSessionClientQuery(
  client$: GramCore,
  request: GetRemoteSessionClientRequest,
  security?: GetRemoteSessionClientSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<RemoteSessionClientQueryData>;
};
export declare function queryKeyRemoteSessionClient(parameters: {
  id: string;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=remoteSessionClient.core.d.ts.map
