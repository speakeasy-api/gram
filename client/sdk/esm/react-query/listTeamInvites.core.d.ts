import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type ListTeamInvitesQueryData = components.ListInvitesResult;
export declare function prefetchListTeamInvites(
  queryClient: QueryClient,
  client$: GramCore,
  request: operations.ListTeamInvitesRequest,
  security?: operations.ListTeamInvitesSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListTeamInvitesQuery(
  client$: GramCore,
  request: operations.ListTeamInvitesRequest,
  security?: operations.ListTeamInvitesSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<ListTeamInvitesQueryData>;
};
export declare function queryKeyListTeamInvites(parameters: {
  organizationId: string;
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listTeamInvites.core.d.ts.map
