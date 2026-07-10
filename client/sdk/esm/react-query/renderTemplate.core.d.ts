import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RenderTemplateResult } from "../models/components/rendertemplateresult.js";
import { RenderTemplateRequest, RenderTemplateSecurity } from "../models/operations/rendertemplate.js";
export type RenderTemplateQueryData = RenderTemplateResult;
export declare function prefetchRenderTemplate(queryClient: QueryClient, client$: GramCore, request: RenderTemplateRequest, security?: RenderTemplateSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRenderTemplateQuery(client$: GramCore, request: RenderTemplateRequest, security?: RenderTemplateSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RenderTemplateQueryData>;
};
export declare function queryKeyRenderTemplate(parameters: {
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=renderTemplate.core.d.ts.map