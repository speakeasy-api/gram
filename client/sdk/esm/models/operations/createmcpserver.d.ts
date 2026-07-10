import * as z from "zod/v4-mini";
import { CreateMcpServerForm, CreateMcpServerForm$Outbound } from "../components/createmcpserverform.js";
export type CreateMcpServerSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type CreateMcpServerSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type CreateMcpServerSecurity = {
    option1?: CreateMcpServerSecurityOption1 | undefined;
    option2?: CreateMcpServerSecurityOption2 | undefined;
};
export type CreateMcpServerRequest = {
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
    createMcpServerForm: CreateMcpServerForm;
};
/** @internal */
export type CreateMcpServerSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateMcpServerSecurityOption1$outboundSchema: z.ZodMiniType<CreateMcpServerSecurityOption1$Outbound, CreateMcpServerSecurityOption1>;
export declare function createMcpServerSecurityOption1ToJSON(createMcpServerSecurityOption1: CreateMcpServerSecurityOption1): string;
/** @internal */
export type CreateMcpServerSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateMcpServerSecurityOption2$outboundSchema: z.ZodMiniType<CreateMcpServerSecurityOption2$Outbound, CreateMcpServerSecurityOption2>;
export declare function createMcpServerSecurityOption2ToJSON(createMcpServerSecurityOption2: CreateMcpServerSecurityOption2): string;
/** @internal */
export type CreateMcpServerSecurity$Outbound = {
    Option1?: CreateMcpServerSecurityOption1$Outbound | undefined;
    Option2?: CreateMcpServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreateMcpServerSecurity$outboundSchema: z.ZodMiniType<CreateMcpServerSecurity$Outbound, CreateMcpServerSecurity>;
export declare function createMcpServerSecurityToJSON(createMcpServerSecurity: CreateMcpServerSecurity): string;
/** @internal */
export type CreateMcpServerRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    CreateMcpServerForm: CreateMcpServerForm$Outbound;
};
/** @internal */
export declare const CreateMcpServerRequest$outboundSchema: z.ZodMiniType<CreateMcpServerRequest$Outbound, CreateMcpServerRequest>;
export declare function createMcpServerRequestToJSON(createMcpServerRequest: CreateMcpServerRequest): string;
//# sourceMappingURL=createmcpserver.d.ts.map