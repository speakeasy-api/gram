import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListProjectsResult } from "../models/components/listprojectsresult.js";
import { ListProjectsRequest, ListProjectsSecurity } from "../models/operations/listprojects.js";
export type ListProjectsQueryData = ListProjectsResult;
export declare function prefetchListProjects(queryClient: QueryClient, client$: GramCore, request: ListProjectsRequest, security?: ListProjectsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListProjectsQuery(client$: GramCore, request: ListProjectsRequest, security?: ListProjectsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListProjectsQueryData>;
};
export declare function queryKeyListProjects(parameters: {
    organizationId: string;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listProjects.core.d.ts.map