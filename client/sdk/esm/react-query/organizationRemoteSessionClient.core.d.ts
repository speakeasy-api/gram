import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import {
  GetOrganizationRemoteSessionClientRequest,
  GetOrganizationRemoteSessionClientSecurity,
} from "../models/operations/getorganizationremotesessionclient.js";
export type OrganizationRemoteSessionClientQueryData = RemoteSessionClient;
export declare function prefetchOrganizationRemoteSessionClient(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetOrganizationRemoteSessionClientRequest,
  security?: GetOrganizationRemoteSessionClientSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildOrganizationRemoteSessionClientQuery(
  client$: GramCore,
  request: GetOrganizationRemoteSessionClientRequest,
  security?: GetOrganizationRemoteSessionClientSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<OrganizationRemoteSessionClientQueryData>;
};
export declare function queryKeyOrganizationRemoteSessionClient(parameters: {
  id: string;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
}): QueryKey;
//# sourceMappingURL=organizationRemoteSessionClient.core.d.ts.map
