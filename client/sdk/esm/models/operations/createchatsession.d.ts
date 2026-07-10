import * as z from "zod/v4-mini";
import { CreateRequestBody, CreateRequestBody$Outbound } from "../components/createrequestbody.js";
export type CreateChatSessionSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type CreateChatSessionSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type CreateChatSessionSecurity = {
    option1?: CreateChatSessionSecurityOption1 | undefined;
    option2?: CreateChatSessionSecurityOption2 | undefined;
};
export type CreateChatSessionRequest = {
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
    createRequestBody: CreateRequestBody;
};
/** @internal */
export type CreateChatSessionSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateChatSessionSecurityOption1$outboundSchema: z.ZodMiniType<CreateChatSessionSecurityOption1$Outbound, CreateChatSessionSecurityOption1>;
export declare function createChatSessionSecurityOption1ToJSON(createChatSessionSecurityOption1: CreateChatSessionSecurityOption1): string;
/** @internal */
export type CreateChatSessionSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateChatSessionSecurityOption2$outboundSchema: z.ZodMiniType<CreateChatSessionSecurityOption2$Outbound, CreateChatSessionSecurityOption2>;
export declare function createChatSessionSecurityOption2ToJSON(createChatSessionSecurityOption2: CreateChatSessionSecurityOption2): string;
/** @internal */
export type CreateChatSessionSecurity$Outbound = {
    Option1?: CreateChatSessionSecurityOption1$Outbound | undefined;
    Option2?: CreateChatSessionSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreateChatSessionSecurity$outboundSchema: z.ZodMiniType<CreateChatSessionSecurity$Outbound, CreateChatSessionSecurity>;
export declare function createChatSessionSecurityToJSON(createChatSessionSecurity: CreateChatSessionSecurity): string;
/** @internal */
export type CreateChatSessionRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    CreateRequestBody: CreateRequestBody$Outbound;
};
/** @internal */
export declare const CreateChatSessionRequest$outboundSchema: z.ZodMiniType<CreateChatSessionRequest$Outbound, CreateChatSessionRequest>;
export declare function createChatSessionRequestToJSON(createChatSessionRequest: CreateChatSessionRequest): string;
//# sourceMappingURL=createchatsession.d.ts.map