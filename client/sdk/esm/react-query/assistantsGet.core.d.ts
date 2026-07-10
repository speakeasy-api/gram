import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Assistant } from "../models/components/assistant.js";
import { GetAssistantRequest, GetAssistantSecurity } from "../models/operations/getassistant.js";
export type AssistantsGetQueryData = Assistant;
export declare function prefetchAssistantsGet(queryClient: QueryClient, client$: GramCore, request: GetAssistantRequest, security?: GetAssistantSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildAssistantsGetQuery(client$: GramCore, request: GetAssistantRequest, security?: GetAssistantSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<AssistantsGetQueryData>;
};
export declare function queryKeyAssistantsGet(parameters: {
    id: string;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=assistantsGet.core.d.ts.map