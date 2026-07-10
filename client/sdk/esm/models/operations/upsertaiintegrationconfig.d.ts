import * as z from "zod/v4-mini";
import { UpsertConfigRequestBody, UpsertConfigRequestBody$Outbound } from "../components/upsertconfigrequestbody.js";
export type UpsertAIIntegrationConfigSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type UpsertAIIntegrationConfigRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    upsertConfigRequestBody: UpsertConfigRequestBody;
};
/** @internal */
export type UpsertAIIntegrationConfigSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpsertAIIntegrationConfigSecurity$outboundSchema: z.ZodMiniType<UpsertAIIntegrationConfigSecurity$Outbound, UpsertAIIntegrationConfigSecurity>;
export declare function upsertAIIntegrationConfigSecurityToJSON(upsertAIIntegrationConfigSecurity: UpsertAIIntegrationConfigSecurity): string;
/** @internal */
export type UpsertAIIntegrationConfigRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    UpsertConfigRequestBody: UpsertConfigRequestBody$Outbound;
};
/** @internal */
export declare const UpsertAIIntegrationConfigRequest$outboundSchema: z.ZodMiniType<UpsertAIIntegrationConfigRequest$Outbound, UpsertAIIntegrationConfigRequest>;
export declare function upsertAIIntegrationConfigRequestToJSON(upsertAIIntegrationConfigRequest: UpsertAIIntegrationConfigRequest): string;
//# sourceMappingURL=upsertaiintegrationconfig.d.ts.map