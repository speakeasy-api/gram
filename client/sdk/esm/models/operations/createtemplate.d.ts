import * as z from "zod/v4-mini";
import { CreatePromptTemplateForm, CreatePromptTemplateForm$Outbound } from "../components/createprompttemplateform.js";
export type CreateTemplateSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type CreateTemplateSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type CreateTemplateSecurity = {
    option1?: CreateTemplateSecurityOption1 | undefined;
    option2?: CreateTemplateSecurityOption2 | undefined;
};
export type CreateTemplateRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    createPromptTemplateForm: CreatePromptTemplateForm;
};
/** @internal */
export type CreateTemplateSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateTemplateSecurityOption1$outboundSchema: z.ZodMiniType<CreateTemplateSecurityOption1$Outbound, CreateTemplateSecurityOption1>;
export declare function createTemplateSecurityOption1ToJSON(createTemplateSecurityOption1: CreateTemplateSecurityOption1): string;
/** @internal */
export type CreateTemplateSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateTemplateSecurityOption2$outboundSchema: z.ZodMiniType<CreateTemplateSecurityOption2$Outbound, CreateTemplateSecurityOption2>;
export declare function createTemplateSecurityOption2ToJSON(createTemplateSecurityOption2: CreateTemplateSecurityOption2): string;
/** @internal */
export type CreateTemplateSecurity$Outbound = {
    Option1?: CreateTemplateSecurityOption1$Outbound | undefined;
    Option2?: CreateTemplateSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreateTemplateSecurity$outboundSchema: z.ZodMiniType<CreateTemplateSecurity$Outbound, CreateTemplateSecurity>;
export declare function createTemplateSecurityToJSON(createTemplateSecurity: CreateTemplateSecurity): string;
/** @internal */
export type CreateTemplateRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    CreatePromptTemplateForm: CreatePromptTemplateForm$Outbound;
};
/** @internal */
export declare const CreateTemplateRequest$outboundSchema: z.ZodMiniType<CreateTemplateRequest$Outbound, CreateTemplateRequest>;
export declare function createTemplateRequestToJSON(createTemplateRequest: CreateTemplateRequest): string;
//# sourceMappingURL=createtemplate.d.ts.map