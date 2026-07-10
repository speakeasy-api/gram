import * as z from "zod/v4-mini";
export type GetOtelForwardingConfigSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type GetOtelForwardingConfigRequest = {
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
export type GetOtelForwardingConfigSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetOtelForwardingConfigSecurity$outboundSchema: z.ZodMiniType<GetOtelForwardingConfigSecurity$Outbound, GetOtelForwardingConfigSecurity>;
export declare function getOtelForwardingConfigSecurityToJSON(getOtelForwardingConfigSecurity: GetOtelForwardingConfigSecurity): string;
/** @internal */
export type GetOtelForwardingConfigRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetOtelForwardingConfigRequest$outboundSchema: z.ZodMiniType<GetOtelForwardingConfigRequest$Outbound, GetOtelForwardingConfigRequest>;
export declare function getOtelForwardingConfigRequestToJSON(getOtelForwardingConfigRequest: GetOtelForwardingConfigRequest): string;
//# sourceMappingURL=getotelforwardingconfig.d.ts.map