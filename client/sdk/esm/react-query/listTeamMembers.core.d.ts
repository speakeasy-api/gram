import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type ListTeamMembersQueryData = components.ListMembersResult;
export declare function prefetchListTeamMembers(
  queryClient: QueryClient,
  client$: GramCore,
  request: operations.ListTeamMembersRequest,
  security?: operations.ListTeamMembersSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListTeamMembersQuery(
  client$: GramCore,
  request: operations.ListTeamMembersRequest,
  security?: operations.ListTeamMembersSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<ListTeamMembersQueryData>;
};
export declare function queryKeyListTeamMembers(parameters: {
  organizationId: string;
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listTeamMembers.core.d.ts.map
