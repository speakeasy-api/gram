import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  ListGlobalRemoteSessionClientsRequest,
  ListGlobalRemoteSessionClientsResponse,
  ListGlobalRemoteSessionClientsSecurity,
} from "../models/operations/listglobalremotesessionclients.js";
import { PageIterator } from "../types/operations.js";
export type GlobalRemoteSessionClientsQueryData =
  ListGlobalRemoteSessionClientsResponse;
export type GlobalRemoteSessionClientsInfiniteQueryData = PageIterator<
  ListGlobalRemoteSessionClientsResponse,
  {
    cursor: string;
  }
>;
export type GlobalRemoteSessionClientsPageParams = PageIterator<
  ListGlobalRemoteSessionClientsResponse,
  {
    cursor: string;
  }
>["~next"];
export declare function prefetchGlobalRemoteSessionClients(
  queryClient: QueryClient,
  client$: GramCore,
  request: ListGlobalRemoteSessionClientsRequest,
  security?: ListGlobalRemoteSessionClientsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function prefetchGlobalRemoteSessionClientsInfinite(
  queryClient: QueryClient,
  client$: GramCore,
  request: ListGlobalRemoteSessionClientsRequest,
  security?: ListGlobalRemoteSessionClientsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGlobalRemoteSessionClientsQuery(
  client$: GramCore,
  request: ListGlobalRemoteSessionClientsRequest,
  security?: ListGlobalRemoteSessionClientsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<GlobalRemoteSessionClientsQueryData>;
};
export declare function buildGlobalRemoteSessionClientsInfiniteQuery(
  client$: GramCore,
  request: ListGlobalRemoteSessionClientsRequest,
  security?: ListGlobalRemoteSessionClientsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext<
      QueryKey,
      GlobalRemoteSessionClientsPageParams
    >,
  ) => Promise<GlobalRemoteSessionClientsInfiniteQueryData>;
};
export declare function queryKeyGlobalRemoteSessionClients(parameters: {
  remoteSessionIssuerId: string;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
}): QueryKey;
export declare function queryKeyGlobalRemoteSessionClientsInfinite(parameters: {
  remoteSessionIssuerId: string;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=globalRemoteSessionClients.core.d.ts.map
