import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetProjectResult } from "../models/components/getprojectresult.js";
import { GetProjectRequest, GetProjectSecurity } from "../models/operations/getproject.js";
export type ProjectQueryData = GetProjectResult;
export declare function prefetchProject(queryClient: QueryClient, client$: GramCore, request: GetProjectRequest, security?: GetProjectSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildProjectQuery(client$: GramCore, request: GetProjectRequest, security?: GetProjectSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ProjectQueryData>;
};
export declare function queryKeyProject(parameters: {
    slug: string;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=project.core.d.ts.map