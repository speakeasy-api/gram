import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type ListAllOrganizationsQueryData = components.ListAllOrganizationsResult;
export declare function prefetchListAllOrganizations(queryClient: QueryClient, client$: GramCore, request?: operations.ListAllOrganizationsRequest | undefined, security?: operations.ListAllOrganizationsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListAllOrganizationsQuery(client$: GramCore, request?: operations.ListAllOrganizationsRequest | undefined, security?: operations.ListAllOrganizationsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListAllOrganizationsQueryData>;
};
export declare function queryKeyListAllOrganizations(parameters: {
    limit?: number | undefined;
    offset?: number | undefined;
    gramKey?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listAllOrganizations.core.d.ts.map