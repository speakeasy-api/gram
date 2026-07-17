import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  ListOrganizationRemoteSessionClientsRequest,
  ListOrganizationRemoteSessionClientsResponse,
  ListOrganizationRemoteSessionClientsSecurity,
} from "../models/operations/listorganizationremotesessionclients.js";
import { PageIterator } from "../types/operations.js";
export type OrganizationRemoteSessionClientsQueryData =
  ListOrganizationRemoteSessionClientsResponse;
export type OrganizationRemoteSessionClientsInfiniteQueryData = PageIterator<
  ListOrganizationRemoteSessionClientsResponse,
  {
    cursor: string;
  }
>;
export type OrganizationRemoteSessionClientsPageParams = PageIterator<
  ListOrganizationRemoteSessionClientsResponse,
  {
    cursor: string;
  }
>["~next"];
export declare function prefetchOrganizationRemoteSessionClients(
  queryClient: QueryClient,
  client$: GramCore,
  request: ListOrganizationRemoteSessionClientsRequest,
  security?: ListOrganizationRemoteSessionClientsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function prefetchOrganizationRemoteSessionClientsInfinite(
  queryClient: QueryClient,
  client$: GramCore,
  request: ListOrganizationRemoteSessionClientsRequest,
  security?: ListOrganizationRemoteSessionClientsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildOrganizationRemoteSessionClientsQuery(
  client$: GramCore,
  request: ListOrganizationRemoteSessionClientsRequest,
  security?: ListOrganizationRemoteSessionClientsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<OrganizationRemoteSessionClientsQueryData>;
};
export declare function buildOrganizationRemoteSessionClientsInfiniteQuery(
  client$: GramCore,
  request: ListOrganizationRemoteSessionClientsRequest,
  security?: ListOrganizationRemoteSessionClientsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext<
      QueryKey,
      OrganizationRemoteSessionClientsPageParams
    >,
  ) => Promise<OrganizationRemoteSessionClientsInfiniteQueryData>;
};
export declare function queryKeyOrganizationRemoteSessionClients(parameters: {
  issuerId: string;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
}): QueryKey;
export declare function queryKeyOrganizationRemoteSessionClientsInfinite(parameters: {
  issuerId: string;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
}): QueryKey;
//# sourceMappingURL=organizationRemoteSessionClients.core.d.ts.map
