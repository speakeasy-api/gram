import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  ListRemoteSessionsRequest,
  ListRemoteSessionsResponse,
  ListRemoteSessionsSecurity,
} from "../models/operations/listremotesessions.js";
import { PageIterator } from "../types/operations.js";
export type RemoteSessionsQueryData = ListRemoteSessionsResponse;
export type RemoteSessionsInfiniteQueryData = PageIterator<
  ListRemoteSessionsResponse,
  {
    cursor: string;
  }
>;
export type RemoteSessionsPageParams = PageIterator<
  ListRemoteSessionsResponse,
  {
    cursor: string;
  }
>["~next"];
export declare function prefetchRemoteSessions(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListRemoteSessionsRequest | undefined,
  security?: ListRemoteSessionsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function prefetchRemoteSessionsInfinite(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListRemoteSessionsRequest | undefined,
  security?: ListRemoteSessionsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildRemoteSessionsQuery(
  client$: GramCore,
  request?: ListRemoteSessionsRequest | undefined,
  security?: ListRemoteSessionsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<RemoteSessionsQueryData>;
};
export declare function buildRemoteSessionsInfiniteQuery(
  client$: GramCore,
  request?: ListRemoteSessionsRequest | undefined,
  security?: ListRemoteSessionsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext<QueryKey, RemoteSessionsPageParams>,
  ) => Promise<RemoteSessionsInfiniteQueryData>;
};
export declare function queryKeyRemoteSessions(parameters: {
  subjectUrn?: string | undefined;
  remoteSessionClientId?: string | undefined;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
export declare function queryKeyRemoteSessionsInfinite(parameters: {
  subjectUrn?: string | undefined;
  remoteSessionClientId?: string | undefined;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=remoteSessions.core.d.ts.map
