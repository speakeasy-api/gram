import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  ListRemoteSessionClientsRequest,
  ListRemoteSessionClientsResponse,
  ListRemoteSessionClientsSecurity,
} from "../models/operations/listremotesessionclients.js";
import { PageIterator } from "../types/operations.js";
export type RemoteSessionClientsQueryData = ListRemoteSessionClientsResponse;
export type RemoteSessionClientsInfiniteQueryData = PageIterator<
  ListRemoteSessionClientsResponse,
  {
    cursor: string;
  }
>;
export type RemoteSessionClientsPageParams = PageIterator<
  ListRemoteSessionClientsResponse,
  {
    cursor: string;
  }
>["~next"];
export declare function prefetchRemoteSessionClients(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListRemoteSessionClientsRequest | undefined,
  security?: ListRemoteSessionClientsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function prefetchRemoteSessionClientsInfinite(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListRemoteSessionClientsRequest | undefined,
  security?: ListRemoteSessionClientsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildRemoteSessionClientsQuery(
  client$: GramCore,
  request?: ListRemoteSessionClientsRequest | undefined,
  security?: ListRemoteSessionClientsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<RemoteSessionClientsQueryData>;
};
export declare function buildRemoteSessionClientsInfiniteQuery(
  client$: GramCore,
  request?: ListRemoteSessionClientsRequest | undefined,
  security?: ListRemoteSessionClientsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext<QueryKey, RemoteSessionClientsPageParams>,
  ) => Promise<RemoteSessionClientsInfiniteQueryData>;
};
export declare function queryKeyRemoteSessionClients(parameters: {
  remoteSessionIssuerId?: string | undefined;
  userSessionIssuerId?: string | undefined;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
export declare function queryKeyRemoteSessionClientsInfinite(parameters: {
  remoteSessionIssuerId?: string | undefined;
  userSessionIssuerId?: string | undefined;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=remoteSessionClients.core.d.ts.map
