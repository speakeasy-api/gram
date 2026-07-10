import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListTriggerDefinitionsResult } from "../models/components/listtriggerdefinitionsresult.js";
import { ListTriggerDefinitionsRequest, ListTriggerDefinitionsSecurity } from "../models/operations/listtriggerdefinitions.js";
export type TriggerDefinitionsQueryData = ListTriggerDefinitionsResult;
export declare function prefetchTriggerDefinitions(queryClient: QueryClient, client$: GramCore, request?: ListTriggerDefinitionsRequest | undefined, security?: ListTriggerDefinitionsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildTriggerDefinitionsQuery(client$: GramCore, request?: ListTriggerDefinitionsRequest | undefined, security?: ListTriggerDefinitionsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<TriggerDefinitionsQueryData>;
};
export declare function queryKeyTriggerDefinitions(parameters: {
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=triggerDefinitions.core.d.ts.map