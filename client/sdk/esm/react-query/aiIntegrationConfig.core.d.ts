import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { AIIntegrationConfig } from "../models/components/aiintegrationconfig.js";
import { GetAIIntegrationConfigRequest, GetAIIntegrationConfigSecurity } from "../models/operations/getaiintegrationconfig.js";
export type AiIntegrationConfigQueryData = AIIntegrationConfig;
export declare function prefetchAiIntegrationConfig(queryClient: QueryClient, client$: GramCore, request: GetAIIntegrationConfigRequest, security?: GetAIIntegrationConfigSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildAiIntegrationConfigQuery(client$: GramCore, request: GetAIIntegrationConfigRequest, security?: GetAIIntegrationConfigSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<AiIntegrationConfigQueryData>;
};
export declare function queryKeyAiIntegrationConfig(parameters: {
    provider: string;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=aiIntegrationConfig.core.d.ts.map