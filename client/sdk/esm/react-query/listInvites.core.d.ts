import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListInvitesResult } from "../models/components/listinvitesresult.js";
import {
  ListInvitesRequest,
  ListInvitesSecurity,
} from "../models/operations/listinvites.js";
export type ListInvitesQueryData = ListInvitesResult;
export declare function prefetchListInvites(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListInvitesRequest | undefined,
  security?: ListInvitesSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListInvitesQuery(
  client$: GramCore,
  request?: ListInvitesRequest | undefined,
  security?: ListInvitesSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<ListInvitesQueryData>;
};
export declare function queryKeyListInvites(parameters: {
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listInvites.core.d.ts.map
