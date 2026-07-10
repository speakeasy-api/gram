import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  ListOrganizationRemoteSessionClientSessionsRequest,
  ListOrganizationRemoteSessionClientSessionsResponse,
  ListOrganizationRemoteSessionClientSessionsSecurity,
} from "../models/operations/listorganizationremotesessionclientsessions.js";
import { PageIterator } from "../types/operations.js";
export type OrganizationRemoteSessionClientSessionsQueryData =
  ListOrganizationRemoteSessionClientSessionsResponse;
export type OrganizationRemoteSessionClientSessionsInfiniteQueryData =
  PageIterator<
    ListOrganizationRemoteSessionClientSessionsResponse,
    {
      cursor: string;
    }
  >;
export type OrganizationRemoteSessionClientSessionsPageParams = PageIterator<
  ListOrganizationRemoteSessionClientSessionsResponse,
  {
    cursor: string;
  }
>["~next"];
export declare function prefetchOrganizationRemoteSessionClientSessions(
  queryClient: QueryClient,
  client$: GramCore,
  request: ListOrganizationRemoteSessionClientSessionsRequest,
  security?: ListOrganizationRemoteSessionClientSessionsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function prefetchOrganizationRemoteSessionClientSessionsInfinite(
  queryClient: QueryClient,
  client$: GramCore,
  request: ListOrganizationRemoteSessionClientSessionsRequest,
  security?: ListOrganizationRemoteSessionClientSessionsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildOrganizationRemoteSessionClientSessionsQuery(
  client$: GramCore,
  request: ListOrganizationRemoteSessionClientSessionsRequest,
  security?: ListOrganizationRemoteSessionClientSessionsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<OrganizationRemoteSessionClientSessionsQueryData>;
};
export declare function buildOrganizationRemoteSessionClientSessionsInfiniteQuery(
  client$: GramCore,
  request: ListOrganizationRemoteSessionClientSessionsRequest,
  security?: ListOrganizationRemoteSessionClientSessionsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext<
      QueryKey,
      OrganizationRemoteSessionClientSessionsPageParams
    >,
  ) => Promise<OrganizationRemoteSessionClientSessionsInfiniteQueryData>;
};
export declare function queryKeyOrganizationRemoteSessionClientSessions(parameters: {
  clientId: string;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
}): QueryKey;
export declare function queryKeyOrganizationRemoteSessionClientSessionsInfinite(parameters: {
  clientId: string;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
}): QueryKey;
//# sourceMappingURL=organizationRemoteSessionClientSessions.core.d.ts.map
