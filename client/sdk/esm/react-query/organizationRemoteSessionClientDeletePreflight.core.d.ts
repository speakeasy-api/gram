import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { OrganizationClientDeletePreflight } from "../models/components/organizationclientdeletepreflight.js";
import {
  GetOrganizationRemoteSessionClientDeletePreflightRequest,
  GetOrganizationRemoteSessionClientDeletePreflightSecurity,
} from "../models/operations/getorganizationremotesessionclientdeletepreflight.js";
export type OrganizationRemoteSessionClientDeletePreflightQueryData =
  OrganizationClientDeletePreflight;
export declare function prefetchOrganizationRemoteSessionClientDeletePreflight(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetOrganizationRemoteSessionClientDeletePreflightRequest,
  security?:
    | GetOrganizationRemoteSessionClientDeletePreflightSecurity
    | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildOrganizationRemoteSessionClientDeletePreflightQuery(
  client$: GramCore,
  request: GetOrganizationRemoteSessionClientDeletePreflightRequest,
  security?:
    | GetOrganizationRemoteSessionClientDeletePreflightSecurity
    | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<OrganizationRemoteSessionClientDeletePreflightQueryData>;
};
export declare function queryKeyOrganizationRemoteSessionClientDeletePreflight(parameters: {
  id: string;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
}): QueryKey;
//# sourceMappingURL=organizationRemoteSessionClientDeletePreflight.core.d.ts.map
