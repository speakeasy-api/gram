import * as z from "zod/v4-mini";
import { AddExternalOAuthServerRequestBody, AddExternalOAuthServerRequestBody$Outbound } from "../components/addexternaloauthserverrequestbody.js";
export type AddExternalOAuthServerSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type AddExternalOAuthServerSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type AddExternalOAuthServerSecurity = {
    option1?: AddExternalOAuthServerSecurityOption1 | undefined;
    option2?: AddExternalOAuthServerSecurityOption2 | undefined;
};
export type AddExternalOAuthServerRequest = {
    /**
     * The slug of the toolset to update
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
    addExternalOAuthServerRequestBody: AddExternalOAuthServerRequestBody;
};
/** @internal */
export type AddExternalOAuthServerSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const AddExternalOAuthServerSecurityOption1$outboundSchema: z.ZodMiniType<AddExternalOAuthServerSecurityOption1$Outbound, AddExternalOAuthServerSecurityOption1>;
export declare function addExternalOAuthServerSecurityOption1ToJSON(addExternalOAuthServerSecurityOption1: AddExternalOAuthServerSecurityOption1): string;
/** @internal */
export type AddExternalOAuthServerSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const AddExternalOAuthServerSecurityOption2$outboundSchema: z.ZodMiniType<AddExternalOAuthServerSecurityOption2$Outbound, AddExternalOAuthServerSecurityOption2>;
export declare function addExternalOAuthServerSecurityOption2ToJSON(addExternalOAuthServerSecurityOption2: AddExternalOAuthServerSecurityOption2): string;
/** @internal */
export type AddExternalOAuthServerSecurity$Outbound = {
    Option1?: AddExternalOAuthServerSecurityOption1$Outbound | undefined;
    Option2?: AddExternalOAuthServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const AddExternalOAuthServerSecurity$outboundSchema: z.ZodMiniType<AddExternalOAuthServerSecurity$Outbound, AddExternalOAuthServerSecurity>;
export declare function addExternalOAuthServerSecurityToJSON(addExternalOAuthServerSecurity: AddExternalOAuthServerSecurity): string;
/** @internal */
export type AddExternalOAuthServerRequest$Outbound = {
    slug: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    AddExternalOAuthServerRequestBody: AddExternalOAuthServerRequestBody$Outbound;
};
/** @internal */
export declare const AddExternalOAuthServerRequest$outboundSchema: z.ZodMiniType<AddExternalOAuthServerRequest$Outbound, AddExternalOAuthServerRequest>;
export declare function addExternalOAuthServerRequestToJSON(addExternalOAuthServerRequest: AddExternalOAuthServerRequest): string;
//# sourceMappingURL=addexternaloauthserver.d.ts.map