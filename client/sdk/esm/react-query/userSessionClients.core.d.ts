import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  ListUserSessionClientsRequest,
  ListUserSessionClientsResponse,
  ListUserSessionClientsSecurity,
} from "../models/operations/listusersessionclients.js";
import { PageIterator } from "../types/operations.js";
export type UserSessionClientsQueryData = ListUserSessionClientsResponse;
export type UserSessionClientsInfiniteQueryData = PageIterator<
  ListUserSessionClientsResponse,
  {
    cursor: string;
  }
>;
export type UserSessionClientsPageParams = PageIterator<
  ListUserSessionClientsResponse,
  {
    cursor: string;
  }
>["~next"];
export declare function prefetchUserSessionClients(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListUserSessionClientsRequest | undefined,
  security?: ListUserSessionClientsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function prefetchUserSessionClientsInfinite(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListUserSessionClientsRequest | undefined,
  security?: ListUserSessionClientsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildUserSessionClientsQuery(
  client$: GramCore,
  request?: ListUserSessionClientsRequest | undefined,
  security?: ListUserSessionClientsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<UserSessionClientsQueryData>;
};
export declare function buildUserSessionClientsInfiniteQuery(
  client$: GramCore,
  request?: ListUserSessionClientsRequest | undefined,
  security?: ListUserSessionClientsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext<QueryKey, UserSessionClientsPageParams>,
  ) => Promise<UserSessionClientsInfiniteQueryData>;
};
export declare function queryKeyUserSessionClients(parameters: {
  userSessionIssuerId?: string | undefined;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
export declare function queryKeyUserSessionClientsInfinite(parameters: {
  userSessionIssuerId?: string | undefined;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=userSessionClients.core.d.ts.map
