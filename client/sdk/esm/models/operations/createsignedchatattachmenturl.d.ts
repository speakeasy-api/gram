import * as z from "zod/v4-mini";
import { CreateSignedChatAttachmentURLForm2, CreateSignedChatAttachmentURLForm2$Outbound } from "../components/createsignedchatattachmenturlform2.js";
export type CreateSignedChatAttachmentURLSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type CreateSignedChatAttachmentURLSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type CreateSignedChatAttachmentURLSecurityOption3 = {
    chatSessionsTokenHeaderGramChatSession: string;
    projectSlugHeaderGramProject: string;
};
export type CreateSignedChatAttachmentURLSecurity = {
    option1?: CreateSignedChatAttachmentURLSecurityOption1 | undefined;
    option2?: CreateSignedChatAttachmentURLSecurityOption2 | undefined;
    option3?: CreateSignedChatAttachmentURLSecurityOption3 | undefined;
};
export type CreateSignedChatAttachmentURLRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * Chat Sessions token header
     */
    gramChatSession?: string | undefined;
    createSignedChatAttachmentURLForm2: CreateSignedChatAttachmentURLForm2;
};
/** @internal */
export type CreateSignedChatAttachmentURLSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateSignedChatAttachmentURLSecurityOption1$outboundSchema: z.ZodMiniType<CreateSignedChatAttachmentURLSecurityOption1$Outbound, CreateSignedChatAttachmentURLSecurityOption1>;
export declare function createSignedChatAttachmentURLSecurityOption1ToJSON(createSignedChatAttachmentURLSecurityOption1: CreateSignedChatAttachmentURLSecurityOption1): string;
/** @internal */
export type CreateSignedChatAttachmentURLSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateSignedChatAttachmentURLSecurityOption2$outboundSchema: z.ZodMiniType<CreateSignedChatAttachmentURLSecurityOption2$Outbound, CreateSignedChatAttachmentURLSecurityOption2>;
export declare function createSignedChatAttachmentURLSecurityOption2ToJSON(createSignedChatAttachmentURLSecurityOption2: CreateSignedChatAttachmentURLSecurityOption2): string;
/** @internal */
export type CreateSignedChatAttachmentURLSecurityOption3$Outbound = {
    "chat_sessions_token_header_Gram-Chat-Session": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateSignedChatAttachmentURLSecurityOption3$outboundSchema: z.ZodMiniType<CreateSignedChatAttachmentURLSecurityOption3$Outbound, CreateSignedChatAttachmentURLSecurityOption3>;
export declare function createSignedChatAttachmentURLSecurityOption3ToJSON(createSignedChatAttachmentURLSecurityOption3: CreateSignedChatAttachmentURLSecurityOption3): string;
/** @internal */
export type CreateSignedChatAttachmentURLSecurity$Outbound = {
    Option1?: CreateSignedChatAttachmentURLSecurityOption1$Outbound | undefined;
    Option2?: CreateSignedChatAttachmentURLSecurityOption2$Outbound | undefined;
    Option3?: CreateSignedChatAttachmentURLSecurityOption3$Outbound | undefined;
};
/** @internal */
export declare const CreateSignedChatAttachmentURLSecurity$outboundSchema: z.ZodMiniType<CreateSignedChatAttachmentURLSecurity$Outbound, CreateSignedChatAttachmentURLSecurity>;
export declare function createSignedChatAttachmentURLSecurityToJSON(createSignedChatAttachmentURLSecurity: CreateSignedChatAttachmentURLSecurity): string;
/** @internal */
export type CreateSignedChatAttachmentURLRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Chat-Session"?: string | undefined;
    CreateSignedChatAttachmentURLForm2: CreateSignedChatAttachmentURLForm2$Outbound;
};
/** @internal */
export declare const CreateSignedChatAttachmentURLRequest$outboundSchema: z.ZodMiniType<CreateSignedChatAttachmentURLRequest$Outbound, CreateSignedChatAttachmentURLRequest>;
export declare function createSignedChatAttachmentURLRequestToJSON(createSignedChatAttachmentURLRequest: CreateSignedChatAttachmentURLRequest): string;
//# sourceMappingURL=createsignedchatattachmenturl.d.ts.map