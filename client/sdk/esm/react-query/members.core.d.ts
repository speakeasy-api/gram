import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListMembersResult } from "../models/components/listmembersresult.js";
import { ListMembersRequest, ListMembersSecurity } from "../models/operations/listmembers.js";
export type MembersQueryData = ListMembersResult;
export declare function prefetchMembers(queryClient: QueryClient, client$: GramCore, request?: ListMembersRequest | undefined, security?: ListMembersSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildMembersQuery(client$: GramCore, request?: ListMembersRequest | undefined, security?: ListMembersSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<MembersQueryData>;
};
export declare function queryKeyMembers(parameters: {
    gramKey?: string | undefined;
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=members.core.d.ts.map