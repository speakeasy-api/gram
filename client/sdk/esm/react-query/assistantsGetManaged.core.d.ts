import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Assistant } from "../models/components/assistant.js";
import { GetManagedAssistantRequest, GetManagedAssistantSecurity } from "../models/operations/getmanagedassistant.js";
export type AssistantsGetManagedQueryData = Assistant;
export declare function prefetchAssistantsGetManaged(queryClient: QueryClient, client$: GramCore, request?: GetManagedAssistantRequest | undefined, security?: GetManagedAssistantSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildAssistantsGetManagedQuery(client$: GramCore, request?: GetManagedAssistantRequest | undefined, security?: GetManagedAssistantSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<AssistantsGetManagedQueryData>;
};
export declare function queryKeyAssistantsGetManaged(parameters: {
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=assistantsGetManaged.core.d.ts.map