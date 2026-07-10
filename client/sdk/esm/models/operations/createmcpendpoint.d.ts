import * as z from "zod/v4-mini";
import { CreateMcpEndpointForm, CreateMcpEndpointForm$Outbound } from "../components/createmcpendpointform.js";
export type CreateMcpEndpointSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type CreateMcpEndpointSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type CreateMcpEndpointSecurity = {
    option1?: CreateMcpEndpointSecurityOption1 | undefined;
    option2?: CreateMcpEndpointSecurityOption2 | undefined;
};
export type CreateMcpEndpointRequest = {
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
    createMcpEndpointForm: CreateMcpEndpointForm;
};
/** @internal */
export type CreateMcpEndpointSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateMcpEndpointSecurityOption1$outboundSchema: z.ZodMiniType<CreateMcpEndpointSecurityOption1$Outbound, CreateMcpEndpointSecurityOption1>;
export declare function createMcpEndpointSecurityOption1ToJSON(createMcpEndpointSecurityOption1: CreateMcpEndpointSecurityOption1): string;
/** @internal */
export type CreateMcpEndpointSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateMcpEndpointSecurityOption2$outboundSchema: z.ZodMiniType<CreateMcpEndpointSecurityOption2$Outbound, CreateMcpEndpointSecurityOption2>;
export declare function createMcpEndpointSecurityOption2ToJSON(createMcpEndpointSecurityOption2: CreateMcpEndpointSecurityOption2): string;
/** @internal */
export type CreateMcpEndpointSecurity$Outbound = {
    Option1?: CreateMcpEndpointSecurityOption1$Outbound | undefined;
    Option2?: CreateMcpEndpointSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreateMcpEndpointSecurity$outboundSchema: z.ZodMiniType<CreateMcpEndpointSecurity$Outbound, CreateMcpEndpointSecurity>;
export declare function createMcpEndpointSecurityToJSON(createMcpEndpointSecurity: CreateMcpEndpointSecurity): string;
/** @internal */
export type CreateMcpEndpointRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    CreateMcpEndpointForm: CreateMcpEndpointForm$Outbound;
};
/** @internal */
export declare const CreateMcpEndpointRequest$outboundSchema: z.ZodMiniType<CreateMcpEndpointRequest$Outbound, CreateMcpEndpointRequest>;
export declare function createMcpEndpointRequestToJSON(createMcpEndpointRequest: CreateMcpEndpointRequest): string;
//# sourceMappingURL=createmcpendpoint.d.ts.map