import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Role } from "../models/components/role.js";
import {
  GetRoleRequest,
  GetRoleSecurity,
} from "../models/operations/getrole.js";
export type RoleQueryData = Role;
export declare function prefetchRole(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetRoleRequest,
  security?: GetRoleSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildRoleQuery(
  client$: GramCore,
  request: GetRoleRequest,
  security?: GetRoleSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<RoleQueryData>;
};
export declare function queryKeyRole(parameters: {
  id: string;
  gramKey?: string | undefined;
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=role.core.d.ts.map
