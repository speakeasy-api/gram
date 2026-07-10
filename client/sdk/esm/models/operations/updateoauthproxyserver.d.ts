import * as z from "zod/v4-mini";
import { UpdateOAuthProxyServerRequestBody, UpdateOAuthProxyServerRequestBody$Outbound } from "../components/updateoauthproxyserverrequestbody.js";
export type UpdateOAuthProxyServerSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type UpdateOAuthProxyServerSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type UpdateOAuthProxyServerSecurity = {
    option1?: UpdateOAuthProxyServerSecurityOption1 | undefined;
    option2?: UpdateOAuthProxyServerSecurityOption2 | undefined;
};
export type UpdateOAuthProxyServerRequest = {
    /**
     * The slug of the toolset whose OAuth proxy server to update
     */
    slug: string;
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
    updateOAuthProxyServerRequestBody: UpdateOAuthProxyServerRequestBody;
};
/** @internal */
export type UpdateOAuthProxyServerSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpdateOAuthProxyServerSecurityOption1$outboundSchema: z.ZodMiniType<UpdateOAuthProxyServerSecurityOption1$Outbound, UpdateOAuthProxyServerSecurityOption1>;
export declare function updateOAuthProxyServerSecurityOption1ToJSON(updateOAuthProxyServerSecurityOption1: UpdateOAuthProxyServerSecurityOption1): string;
/** @internal */
export type UpdateOAuthProxyServerSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpdateOAuthProxyServerSecurityOption2$outboundSchema: z.ZodMiniType<UpdateOAuthProxyServerSecurityOption2$Outbound, UpdateOAuthProxyServerSecurityOption2>;
export declare function updateOAuthProxyServerSecurityOption2ToJSON(updateOAuthProxyServerSecurityOption2: UpdateOAuthProxyServerSecurityOption2): string;
/** @internal */
export type UpdateOAuthProxyServerSecurity$Outbound = {
    Option1?: UpdateOAuthProxyServerSecurityOption1$Outbound | undefined;
    Option2?: UpdateOAuthProxyServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpdateOAuthProxyServerSecurity$outboundSchema: z.ZodMiniType<UpdateOAuthProxyServerSecurity$Outbound, UpdateOAuthProxyServerSecurity>;
export declare function updateOAuthProxyServerSecurityToJSON(updateOAuthProxyServerSecurity: UpdateOAuthProxyServerSecurity): string;
/** @internal */
export type UpdateOAuthProxyServerRequest$Outbound = {
    slug: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    UpdateOAuthProxyServerRequestBody: UpdateOAuthProxyServerRequestBody$Outbound;
};
/** @internal */
export declare const UpdateOAuthProxyServerRequest$outboundSchema: z.ZodMiniType<UpdateOAuthProxyServerRequest$Outbound, UpdateOAuthProxyServerRequest>;
export declare function updateOAuthProxyServerRequestToJSON(updateOAuthProxyServerRequest: UpdateOAuthProxyServerRequest): string;
//# sourceMappingURL=updateoauthproxyserver.d.ts.map