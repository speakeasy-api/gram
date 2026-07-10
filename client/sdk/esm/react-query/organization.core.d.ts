import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Organization } from "../models/components/organization.js";
import {
  GetOrganizationRequest,
  GetOrganizationSecurity,
} from "../models/operations/getorganization.js";
export type OrganizationQueryData = Organization;
export declare function prefetchOrganization(
  queryClient: QueryClient,
  client$: GramCore,
  request?: GetOrganizationRequest | undefined,
  security?: GetOrganizationSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildOrganizationQuery(
  client$: GramCore,
  request?: GetOrganizationRequest | undefined,
  security?: GetOrganizationSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<OrganizationQueryData>;
};
export declare function queryKeyOrganization(parameters: {
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=organization.core.d.ts.map
