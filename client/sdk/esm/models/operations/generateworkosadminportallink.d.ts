import * as z from "zod/v4-mini";
import { GenerateWorkOSAdminPortalLinkRequestBody, GenerateWorkOSAdminPortalLinkRequestBody$Outbound } from "../components/generateworkosadminportallinkrequestbody.js";
export type GenerateWorkOSAdminPortalLinkSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type GenerateWorkOSAdminPortalLinkRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    generateWorkOSAdminPortalLinkRequestBody: GenerateWorkOSAdminPortalLinkRequestBody;
};
/** @internal */
export type GenerateWorkOSAdminPortalLinkSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GenerateWorkOSAdminPortalLinkSecurity$outboundSchema: z.ZodMiniType<GenerateWorkOSAdminPortalLinkSecurity$Outbound, GenerateWorkOSAdminPortalLinkSecurity>;
export declare function generateWorkOSAdminPortalLinkSecurityToJSON(generateWorkOSAdminPortalLinkSecurity: GenerateWorkOSAdminPortalLinkSecurity): string;
/** @internal */
export type GenerateWorkOSAdminPortalLinkRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    GenerateWorkOSAdminPortalLinkRequestBody: GenerateWorkOSAdminPortalLinkRequestBody$Outbound;
};
/** @internal */
export declare const GenerateWorkOSAdminPortalLinkRequest$outboundSchema: z.ZodMiniType<GenerateWorkOSAdminPortalLinkRequest$Outbound, GenerateWorkOSAdminPortalLinkRequest>;
export declare function generateWorkOSAdminPortalLinkRequestToJSON(generateWorkOSAdminPortalLinkRequest: GenerateWorkOSAdminPortalLinkRequest): string;
//# sourceMappingURL=generateworkosadminportallink.d.ts.map