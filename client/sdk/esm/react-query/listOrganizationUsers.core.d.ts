import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListUsersResult } from "../models/components/listusersresult.js";
import { ListOrganizationUsersRequest, ListOrganizationUsersSecurity } from "../models/operations/listorganizationusers.js";
export type ListOrganizationUsersQueryData = ListUsersResult;
export declare function prefetchListOrganizationUsers(queryClient: QueryClient, client$: GramCore, request?: ListOrganizationUsersRequest | undefined, security?: ListOrganizationUsersSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListOrganizationUsersQuery(client$: GramCore, request?: ListOrganizationUsersRequest | undefined, security?: ListOrganizationUsersSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListOrganizationUsersQueryData>;
};
export declare function queryKeyListOrganizationUsers(parameters: {
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listOrganizationUsers.core.d.ts.map