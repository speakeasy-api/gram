import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRolesResult } from "../models/components/listrolesresult.js";
import {
  ListRolesRequest,
  ListRolesSecurity,
} from "../models/operations/listroles.js";
export type RolesQueryData = ListRolesResult;
export declare function prefetchRoles(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListRolesRequest | undefined,
  security?: ListRolesSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildRolesQuery(
  client$: GramCore,
  request?: ListRolesRequest | undefined,
  security?: ListRolesSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<RolesQueryData>;
};
export declare function queryKeyRoles(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=roles.core.d.ts.map
