import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ServeOpenAPIv3Request, ServeOpenAPIv3Response, ServeOpenAPIv3Security } from "../models/operations/serveopenapiv3.js";
export type ServeOpenAPIv3QueryData = ServeOpenAPIv3Response;
export declare function prefetchServeOpenAPIv3(queryClient: QueryClient, client$: GramCore, request: ServeOpenAPIv3Request, security?: ServeOpenAPIv3Security | undefined, options?: RequestOptions): Promise<void>;
export declare function buildServeOpenAPIv3Query(client$: GramCore, request: ServeOpenAPIv3Request, security?: ServeOpenAPIv3Security | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ServeOpenAPIv3QueryData>;
};
export declare function queryKeyServeOpenAPIv3(parameters: {
    id: string;
    projectId: string;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=serveOpenAPIv3.core.d.ts.map