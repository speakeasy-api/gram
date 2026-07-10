import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  ListUserSessionIssuersRequest,
  ListUserSessionIssuersResponse,
  ListUserSessionIssuersSecurity,
} from "../models/operations/listusersessionissuers.js";
import { PageIterator } from "../types/operations.js";
export type UserSessionIssuersQueryData = ListUserSessionIssuersResponse;
export type UserSessionIssuersInfiniteQueryData = PageIterator<
  ListUserSessionIssuersResponse,
  {
    cursor: string;
  }
>;
export type UserSessionIssuersPageParams = PageIterator<
  ListUserSessionIssuersResponse,
  {
    cursor: string;
  }
>["~next"];
export declare function prefetchUserSessionIssuers(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListUserSessionIssuersRequest | undefined,
  security?: ListUserSessionIssuersSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function prefetchUserSessionIssuersInfinite(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListUserSessionIssuersRequest | undefined,
  security?: ListUserSessionIssuersSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildUserSessionIssuersQuery(
  client$: GramCore,
  request?: ListUserSessionIssuersRequest | undefined,
  security?: ListUserSessionIssuersSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<UserSessionIssuersQueryData>;
};
export declare function buildUserSessionIssuersInfiniteQuery(
  client$: GramCore,
  request?: ListUserSessionIssuersRequest | undefined,
  security?: ListUserSessionIssuersSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext<QueryKey, UserSessionIssuersPageParams>,
  ) => Promise<UserSessionIssuersInfiniteQueryData>;
};
export declare function queryKeyUserSessionIssuers(parameters: {
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
export declare function queryKeyUserSessionIssuersInfinite(parameters: {
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=userSessionIssuers.core.d.ts.map
