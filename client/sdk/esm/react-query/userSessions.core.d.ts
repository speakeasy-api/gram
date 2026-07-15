import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  ListUserSessionsQueryParamStatus,
  ListUserSessionsRequest,
  ListUserSessionsResponse,
  ListUserSessionsSecurity,
} from "../models/operations/listusersessions.js";
import { PageIterator } from "../types/operations.js";
export type UserSessionsQueryData = ListUserSessionsResponse;
export type UserSessionsInfiniteQueryData = PageIterator<
  ListUserSessionsResponse,
  {
    cursor: string;
  }
>;
export type UserSessionsPageParams = PageIterator<
  ListUserSessionsResponse,
  {
    cursor: string;
  }
>["~next"];
export declare function prefetchUserSessions(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListUserSessionsRequest | undefined,
  security?: ListUserSessionsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function prefetchUserSessionsInfinite(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListUserSessionsRequest | undefined,
  security?: ListUserSessionsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildUserSessionsQuery(
  client$: GramCore,
  request?: ListUserSessionsRequest | undefined,
  security?: ListUserSessionsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<UserSessionsQueryData>;
};
export declare function buildUserSessionsInfiniteQuery(
  client$: GramCore,
  request?: ListUserSessionsRequest | undefined,
  security?: ListUserSessionsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext<QueryKey, UserSessionsPageParams>,
  ) => Promise<UserSessionsInfiniteQueryData>;
};
export declare function queryKeyUserSessions(parameters: {
  subjectUrn?: string | undefined;
  userSessionIssuerId?: string | undefined;
  status?: ListUserSessionsQueryParamStatus | undefined;
  clientId?: string | undefined;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
export declare function queryKeyUserSessionsInfinite(parameters: {
  subjectUrn?: string | undefined;
  userSessionIssuerId?: string | undefined;
  status?: ListUserSessionsQueryParamStatus | undefined;
  clientId?: string | undefined;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=userSessions.core.d.ts.map
