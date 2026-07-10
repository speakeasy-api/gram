import * as z from "zod/v4-mini";
export type ClearMCPRegistryCacheSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ClearMCPRegistryCacheSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ClearMCPRegistryCacheSecurity = {
    option1?: ClearMCPRegistryCacheSecurityOption1 | undefined;
    option2?: ClearMCPRegistryCacheSecurityOption2 | undefined;
};
export type ClearMCPRegistryCacheRequest = {
    /**
     * The registry to clear cache for
     */
    registryId: string;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
};
/** @internal */
export type ClearMCPRegistryCacheSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ClearMCPRegistryCacheSecurityOption1$outboundSchema: z.ZodMiniType<ClearMCPRegistryCacheSecurityOption1$Outbound, ClearMCPRegistryCacheSecurityOption1>;
export declare function clearMCPRegistryCacheSecurityOption1ToJSON(clearMCPRegistryCacheSecurityOption1: ClearMCPRegistryCacheSecurityOption1): string;
/** @internal */
export type ClearMCPRegistryCacheSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ClearMCPRegistryCacheSecurityOption2$outboundSchema: z.ZodMiniType<ClearMCPRegistryCacheSecurityOption2$Outbound, ClearMCPRegistryCacheSecurityOption2>;
export declare function clearMCPRegistryCacheSecurityOption2ToJSON(clearMCPRegistryCacheSecurityOption2: ClearMCPRegistryCacheSecurityOption2): string;
/** @internal */
export type ClearMCPRegistryCacheSecurity$Outbound = {
    Option1?: ClearMCPRegistryCacheSecurityOption1$Outbound | undefined;
    Option2?: ClearMCPRegistryCacheSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ClearMCPRegistryCacheSecurity$outboundSchema: z.ZodMiniType<ClearMCPRegistryCacheSecurity$Outbound, ClearMCPRegistryCacheSecurity>;
export declare function clearMCPRegistryCacheSecurityToJSON(clearMCPRegistryCacheSecurity: ClearMCPRegistryCacheSecurity): string;
/** @internal */
export type ClearMCPRegistryCacheRequest$Outbound = {
    registry_id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ClearMCPRegistryCacheRequest$outboundSchema: z.ZodMiniType<ClearMCPRegistryCacheRequest$Outbound, ClearMCPRegistryCacheRequest>;
export declare function clearMCPRegistryCacheRequestToJSON(clearMCPRegistryCacheRequest: ClearMCPRegistryCacheRequest): string;
//# sourceMappingURL=clearmcpregistrycache.d.ts.map