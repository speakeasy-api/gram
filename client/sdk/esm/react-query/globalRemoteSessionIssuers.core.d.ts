import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  ListGlobalRemoteSessionIssuersRequest,
  ListGlobalRemoteSessionIssuersResponse,
  ListGlobalRemoteSessionIssuersSecurity,
} from "../models/operations/listglobalremotesessionissuers.js";
import { PageIterator } from "../types/operations.js";
export type GlobalRemoteSessionIssuersQueryData =
  ListGlobalRemoteSessionIssuersResponse;
export type GlobalRemoteSessionIssuersInfiniteQueryData = PageIterator<
  ListGlobalRemoteSessionIssuersResponse,
  {
    cursor: string;
  }
>;
export type GlobalRemoteSessionIssuersPageParams = PageIterator<
  ListGlobalRemoteSessionIssuersResponse,
  {
    cursor: string;
  }
>["~next"];
export declare function prefetchGlobalRemoteSessionIssuers(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListGlobalRemoteSessionIssuersRequest | undefined,
  security?: ListGlobalRemoteSessionIssuersSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function prefetchGlobalRemoteSessionIssuersInfinite(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListGlobalRemoteSessionIssuersRequest | undefined,
  security?: ListGlobalRemoteSessionIssuersSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGlobalRemoteSessionIssuersQuery(
  client$: GramCore,
  request?: ListGlobalRemoteSessionIssuersRequest | undefined,
  security?: ListGlobalRemoteSessionIssuersSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<GlobalRemoteSessionIssuersQueryData>;
};
export declare function buildGlobalRemoteSessionIssuersInfiniteQuery(
  client$: GramCore,
  request?: ListGlobalRemoteSessionIssuersRequest | undefined,
  security?: ListGlobalRemoteSessionIssuersSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext<
      QueryKey,
      GlobalRemoteSessionIssuersPageParams
    >,
  ) => Promise<GlobalRemoteSessionIssuersInfiniteQueryData>;
};
export declare function queryKeyGlobalRemoteSessionIssuers(parameters: {
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
}): QueryKey;
export declare function queryKeyGlobalRemoteSessionIssuersInfinite(parameters: {
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=globalRemoteSessionIssuers.core.d.ts.map
