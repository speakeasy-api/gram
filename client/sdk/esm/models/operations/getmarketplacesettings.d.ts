import * as z from "zod/v4-mini";
export type GetMarketplaceSettingsSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type GetMarketplaceSettingsRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
};
/** @internal */
export type GetMarketplaceSettingsSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetMarketplaceSettingsSecurity$outboundSchema: z.ZodMiniType<GetMarketplaceSettingsSecurity$Outbound, GetMarketplaceSettingsSecurity>;
export declare function getMarketplaceSettingsSecurityToJSON(getMarketplaceSettingsSecurity: GetMarketplaceSettingsSecurity): string;
/** @internal */
export type GetMarketplaceSettingsRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetMarketplaceSettingsRequest$outboundSchema: z.ZodMiniType<GetMarketplaceSettingsRequest$Outbound, GetMarketplaceSettingsRequest>;
export declare function getMarketplaceSettingsRequestToJSON(getMarketplaceSettingsRequest: GetMarketplaceSettingsRequest): string;
//# sourceMappingURL=getmarketplacesettings.d.ts.map