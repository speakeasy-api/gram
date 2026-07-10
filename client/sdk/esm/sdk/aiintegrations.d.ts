import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { AIIntegrationConfig } from "../models/components/aiintegrationconfig.js";
import { DeleteAIIntegrationConfigRequest, DeleteAIIntegrationConfigSecurity } from "../models/operations/deleteaiintegrationconfig.js";
import { GetAIIntegrationConfigRequest, GetAIIntegrationConfigSecurity } from "../models/operations/getaiintegrationconfig.js";
import { UpsertAIIntegrationConfigRequest, UpsertAIIntegrationConfigSecurity } from "../models/operations/upsertaiintegrationconfig.js";
export declare class AiIntegrations extends ClientSDK {
    /**
     * deleteConfig aiIntegrations
     *
     * @remarks
     * Delete the org-wide AI integration config for a provider.
     */
    deleteConfig(request: DeleteAIIntegrationConfigRequest, security?: DeleteAIIntegrationConfigSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * getConfig aiIntegrations
     *
     * @remarks
     * Get the org-wide AI integration config for a provider. Returns an empty config (enabled=false, has_api_key=false) when none is set.
     */
    getConfig(request: GetAIIntegrationConfigRequest, security?: GetAIIntegrationConfigSecurity | undefined, options?: RequestOptions): Promise<AIIntegrationConfig>;
    /**
     * upsertConfig aiIntegrations
     *
     * @remarks
     * Create or update the org-wide AI integration config for a provider.
     */
    upsertConfig(request: UpsertAIIntegrationConfigRequest, security?: UpsertAIIntegrationConfigSecurity | undefined, options?: RequestOptions): Promise<AIIntegrationConfig>;
}
//# sourceMappingURL=aiintegrations.d.ts.map