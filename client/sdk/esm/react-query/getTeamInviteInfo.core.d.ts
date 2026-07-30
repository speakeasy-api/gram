import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type GetTeamInviteInfoQueryData = components.InviteInfoResult;
export declare function prefetchGetTeamInviteInfo(
  queryClient: QueryClient,
  client$: GramCore,
  request: operations.GetTeamInviteInfoRequest,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetTeamInviteInfoQuery(
  client$: GramCore,
  request: operations.GetTeamInviteInfoRequest,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<GetTeamInviteInfoQueryData>;
};
export declare function queryKeyGetTeamInviteInfo(parameters: {
  token: string;
}): QueryKey;
//# sourceMappingURL=getTeamInviteInfo.core.d.ts.map
