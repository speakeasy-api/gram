import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RenderTemplateResult } from "../models/components/rendertemplateresult.js";
import { RenderTemplateByIDRequest, RenderTemplateByIDSecurity } from "../models/operations/rendertemplatebyid.js";
export type RenderTemplateByIDQueryData = RenderTemplateResult;
export declare function prefetchRenderTemplateByID(queryClient: QueryClient, client$: GramCore, request: RenderTemplateByIDRequest, security?: RenderTemplateByIDSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRenderTemplateByIDQuery(client$: GramCore, request: RenderTemplateByIDRequest, security?: RenderTemplateByIDSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RenderTemplateByIDQueryData>;
};
export declare function queryKeyRenderTemplateByID(parameters: {
    id: string;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=renderTemplateByID.core.d.ts.map