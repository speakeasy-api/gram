import * as z from "zod/v4-mini";
import { UpdateMarketplaceSettingsRequestBody, UpdateMarketplaceSettingsRequestBody$Outbound } from "../components/updatemarketplacesettingsrequestbody.js";
export type UpdateMarketplaceSettingsSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type UpdateMarketplaceSettingsRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    updateMarketplaceSettingsRequestBody: UpdateMarketplaceSettingsRequestBody;
};
/** @internal */
export type UpdateMarketplaceSettingsSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateMarketplaceSettingsSecurity$outboundSchema: z.ZodMiniType<UpdateMarketplaceSettingsSecurity$Outbound, UpdateMarketplaceSettingsSecurity>;
export declare function updateMarketplaceSettingsSecurityToJSON(updateMarketplaceSettingsSecurity: UpdateMarketplaceSettingsSecurity): string;
/** @internal */
export type UpdateMarketplaceSettingsRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    UpdateMarketplaceSettingsRequestBody: UpdateMarketplaceSettingsRequestBody$Outbound;
};
/** @internal */
export declare const UpdateMarketplaceSettingsRequest$outboundSchema: z.ZodMiniType<UpdateMarketplaceSettingsRequest$Outbound, UpdateMarketplaceSettingsRequest>;
export declare function updateMarketplaceSettingsRequestToJSON(updateMarketplaceSettingsRequest: UpdateMarketplaceSettingsRequest): string;
//# sourceMappingURL=updatemarketplacesettings.d.ts.map