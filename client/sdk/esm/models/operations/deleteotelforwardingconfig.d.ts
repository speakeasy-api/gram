import * as z from "zod/v4-mini";
export type DeleteOtelForwardingConfigSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type DeleteOtelForwardingConfigRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type DeleteOtelForwardingConfigSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteOtelForwardingConfigSecurity$outboundSchema: z.ZodMiniType<DeleteOtelForwardingConfigSecurity$Outbound, DeleteOtelForwardingConfigSecurity>;
export declare function deleteOtelForwardingConfigSecurityToJSON(deleteOtelForwardingConfigSecurity: DeleteOtelForwardingConfigSecurity): string;
/** @internal */
export type DeleteOtelForwardingConfigRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteOtelForwardingConfigRequest$outboundSchema: z.ZodMiniType<DeleteOtelForwardingConfigRequest$Outbound, DeleteOtelForwardingConfigRequest>;
export declare function deleteOtelForwardingConfigRequestToJSON(deleteOtelForwardingConfigRequest: DeleteOtelForwardingConfigRequest): string;
//# sourceMappingURL=deleteotelforwardingconfig.d.ts.map