import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListPromptTemplatesResult } from "../models/components/listprompttemplatesresult.js";
import { ListTemplatesRequest, ListTemplatesSecurity } from "../models/operations/listtemplates.js";
export type TemplatesQueryData = ListPromptTemplatesResult;
export declare function prefetchTemplates(queryClient: QueryClient, client$: GramCore, request?: ListTemplatesRequest | undefined, security?: ListTemplatesSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildTemplatesQuery(client$: GramCore, request?: ListTemplatesRequest | undefined, security?: ListTemplatesSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<TemplatesQueryData>;
};
export declare function queryKeyTemplates(parameters: {
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=templates.core.d.ts.map