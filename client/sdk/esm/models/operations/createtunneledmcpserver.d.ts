import * as z from "zod/v4-mini";
import { CreateTunneledMcpServerForm, CreateTunneledMcpServerForm$Outbound } from "../components/createtunneledmcpserverform.js";
export type CreateTunneledMcpServerSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type CreateTunneledMcpServerSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type CreateTunneledMcpServerSecurity = {
    option1?: CreateTunneledMcpServerSecurityOption1 | undefined;
    option2?: CreateTunneledMcpServerSecurityOption2 | undefined;
};
export type CreateTunneledMcpServerRequest = {
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
    createTunneledMcpServerForm: CreateTunneledMcpServerForm;
};
/** @internal */
export type CreateTunneledMcpServerSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateTunneledMcpServerSecurityOption1$outboundSchema: z.ZodMiniType<CreateTunneledMcpServerSecurityOption1$Outbound, CreateTunneledMcpServerSecurityOption1>;
export declare function createTunneledMcpServerSecurityOption1ToJSON(createTunneledMcpServerSecurityOption1: CreateTunneledMcpServerSecurityOption1): string;
/** @internal */
export type CreateTunneledMcpServerSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateTunneledMcpServerSecurityOption2$outboundSchema: z.ZodMiniType<CreateTunneledMcpServerSecurityOption2$Outbound, CreateTunneledMcpServerSecurityOption2>;
export declare function createTunneledMcpServerSecurityOption2ToJSON(createTunneledMcpServerSecurityOption2: CreateTunneledMcpServerSecurityOption2): string;
/** @internal */
export type CreateTunneledMcpServerSecurity$Outbound = {
    Option1?: CreateTunneledMcpServerSecurityOption1$Outbound | undefined;
    Option2?: CreateTunneledMcpServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreateTunneledMcpServerSecurity$outboundSchema: z.ZodMiniType<CreateTunneledMcpServerSecurity$Outbound, CreateTunneledMcpServerSecurity>;
export declare function createTunneledMcpServerSecurityToJSON(createTunneledMcpServerSecurity: CreateTunneledMcpServerSecurity): string;
/** @internal */
export type CreateTunneledMcpServerRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    CreateTunneledMcpServerForm: CreateTunneledMcpServerForm$Outbound;
};
/** @internal */
export declare const CreateTunneledMcpServerRequest$outboundSchema: z.ZodMiniType<CreateTunneledMcpServerRequest$Outbound, CreateTunneledMcpServerRequest>;
export declare function createTunneledMcpServerRequestToJSON(createTunneledMcpServerRequest: CreateTunneledMcpServerRequest): string;
//# sourceMappingURL=createtunneledmcpserver.d.ts.map