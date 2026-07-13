import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type GetInviteByTokenQueryData = components.OrganizationInvitationAccept;
export declare function prefetchGetInviteByToken(
  queryClient: QueryClient,
  client$: GramCore,
  request: operations.GetInviteByTokenRequest,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetInviteByTokenQuery(
  client$: GramCore,
  request: operations.GetInviteByTokenRequest,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<GetInviteByTokenQueryData>;
};
export declare function queryKeyGetInviteByToken(parameters: {
  token: string;
}): QueryKey;
//# sourceMappingURL=getInviteByToken.core.d.ts.map
