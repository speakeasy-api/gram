import * as z from "zod/v4-mini";
import { DeleteConfigRequestBody, DeleteConfigRequestBody$Outbound } from "../components/deleteconfigrequestbody.js";
export type DeleteAIIntegrationConfigSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type DeleteAIIntegrationConfigRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    deleteConfigRequestBody: DeleteConfigRequestBody;
};
/** @internal */
export type DeleteAIIntegrationConfigSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteAIIntegrationConfigSecurity$outboundSchema: z.ZodMiniType<DeleteAIIntegrationConfigSecurity$Outbound, DeleteAIIntegrationConfigSecurity>;
export declare function deleteAIIntegrationConfigSecurityToJSON(deleteAIIntegrationConfigSecurity: DeleteAIIntegrationConfigSecurity): string;
/** @internal */
export type DeleteAIIntegrationConfigRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    DeleteConfigRequestBody: DeleteConfigRequestBody$Outbound;
};
/** @internal */
export declare const DeleteAIIntegrationConfigRequest$outboundSchema: z.ZodMiniType<DeleteAIIntegrationConfigRequest$Outbound, DeleteAIIntegrationConfigRequest>;
export declare function deleteAIIntegrationConfigRequestToJSON(deleteAIIntegrationConfigRequest: DeleteAIIntegrationConfigRequest): string;
//# sourceMappingURL=deleteaiintegrationconfig.d.ts.map