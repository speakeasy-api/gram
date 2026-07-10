import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetPromptTemplateResult } from "../models/components/getprompttemplateresult.js";
import {
  GetTemplateRequest,
  GetTemplateSecurity,
} from "../models/operations/gettemplate.js";
export type TemplateQueryData = GetPromptTemplateResult;
export declare function prefetchTemplate(
  queryClient: QueryClient,
  client$: GramCore,
  request?: GetTemplateRequest | undefined,
  security?: GetTemplateSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildTemplateQuery(
  client$: GramCore,
  request?: GetTemplateRequest | undefined,
  security?: GetTemplateSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<TemplateQueryData>;
};
export declare function queryKeyTemplate(parameters: {
  id?: string | undefined;
  name?: string | undefined;
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=template.core.d.ts.map
