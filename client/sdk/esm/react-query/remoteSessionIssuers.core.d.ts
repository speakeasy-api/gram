import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  ListRemoteSessionIssuersRequest,
  ListRemoteSessionIssuersResponse,
  ListRemoteSessionIssuersSecurity,
} from "../models/operations/listremotesessionissuers.js";
import { PageIterator } from "../types/operations.js";
export type RemoteSessionIssuersQueryData = ListRemoteSessionIssuersResponse;
export type RemoteSessionIssuersInfiniteQueryData = PageIterator<
  ListRemoteSessionIssuersResponse,
  {
    cursor: string;
  }
>;
export type RemoteSessionIssuersPageParams = PageIterator<
  ListRemoteSessionIssuersResponse,
  {
    cursor: string;
  }
>["~next"];
export declare function prefetchRemoteSessionIssuers(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListRemoteSessionIssuersRequest | undefined,
  security?: ListRemoteSessionIssuersSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function prefetchRemoteSessionIssuersInfinite(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListRemoteSessionIssuersRequest | undefined,
  security?: ListRemoteSessionIssuersSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildRemoteSessionIssuersQuery(
  client$: GramCore,
  request?: ListRemoteSessionIssuersRequest | undefined,
  security?: ListRemoteSessionIssuersSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<RemoteSessionIssuersQueryData>;
};
export declare function buildRemoteSessionIssuersInfiniteQuery(
  client$: GramCore,
  request?: ListRemoteSessionIssuersRequest | undefined,
  security?: ListRemoteSessionIssuersSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext<QueryKey, RemoteSessionIssuersPageParams>,
  ) => Promise<RemoteSessionIssuersInfiniteQueryData>;
};
export declare function queryKeyRemoteSessionIssuers(parameters: {
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
export declare function queryKeyRemoteSessionIssuersInfinite(parameters: {
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=remoteSessionIssuers.core.d.ts.map
