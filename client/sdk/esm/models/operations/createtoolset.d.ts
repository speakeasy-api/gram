import * as z from "zod/v4-mini";
import { CreateToolsetRequestBody, CreateToolsetRequestBody$Outbound } from "../components/createtoolsetrequestbody.js";
export type CreateToolsetSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type CreateToolsetSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type CreateToolsetSecurity = {
    option1?: CreateToolsetSecurityOption1 | undefined;
    option2?: CreateToolsetSecurityOption2 | undefined;
};
export type CreateToolsetRequest = {
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
    createToolsetRequestBody: CreateToolsetRequestBody;
};
/** @internal */
export type CreateToolsetSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateToolsetSecurityOption1$outboundSchema: z.ZodMiniType<CreateToolsetSecurityOption1$Outbound, CreateToolsetSecurityOption1>;
export declare function createToolsetSecurityOption1ToJSON(createToolsetSecurityOption1: CreateToolsetSecurityOption1): string;
/** @internal */
export type CreateToolsetSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateToolsetSecurityOption2$outboundSchema: z.ZodMiniType<CreateToolsetSecurityOption2$Outbound, CreateToolsetSecurityOption2>;
export declare function createToolsetSecurityOption2ToJSON(createToolsetSecurityOption2: CreateToolsetSecurityOption2): string;
/** @internal */
export type CreateToolsetSecurity$Outbound = {
    Option1?: CreateToolsetSecurityOption1$Outbound | undefined;
    Option2?: CreateToolsetSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreateToolsetSecurity$outboundSchema: z.ZodMiniType<CreateToolsetSecurity$Outbound, CreateToolsetSecurity>;
export declare function createToolsetSecurityToJSON(createToolsetSecurity: CreateToolsetSecurity): string;
/** @internal */
export type CreateToolsetRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    CreateToolsetRequestBody: CreateToolsetRequestBody$Outbound;
};
/** @internal */
export declare const CreateToolsetRequest$outboundSchema: z.ZodMiniType<CreateToolsetRequest$Outbound, CreateToolsetRequest>;
export declare function createToolsetRequestToJSON(createToolsetRequest: CreateToolsetRequest): string;
//# sourceMappingURL=createtoolset.d.ts.map