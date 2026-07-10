import * as z from "zod/v4-mini";
export type DeleteTemplateSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type DeleteTemplateSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type DeleteTemplateSecurity = {
    option1?: DeleteTemplateSecurityOption1 | undefined;
    option2?: DeleteTemplateSecurityOption2 | undefined;
};
export type DeleteTemplateRequest = {
    /**
     * The ID of the prompt template
     */
    id?: string | undefined;
    /**
     * The name of the prompt template
     */
    name?: string | undefined;
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
};
/** @internal */
export type DeleteTemplateSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const DeleteTemplateSecurityOption1$outboundSchema: z.ZodMiniType<DeleteTemplateSecurityOption1$Outbound, DeleteTemplateSecurityOption1>;
export declare function deleteTemplateSecurityOption1ToJSON(deleteTemplateSecurityOption1: DeleteTemplateSecurityOption1): string;
/** @internal */
export type DeleteTemplateSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DeleteTemplateSecurityOption2$outboundSchema: z.ZodMiniType<DeleteTemplateSecurityOption2$Outbound, DeleteTemplateSecurityOption2>;
export declare function deleteTemplateSecurityOption2ToJSON(deleteTemplateSecurityOption2: DeleteTemplateSecurityOption2): string;
/** @internal */
export type DeleteTemplateSecurity$Outbound = {
    Option1?: DeleteTemplateSecurityOption1$Outbound | undefined;
    Option2?: DeleteTemplateSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DeleteTemplateSecurity$outboundSchema: z.ZodMiniType<DeleteTemplateSecurity$Outbound, DeleteTemplateSecurity>;
export declare function deleteTemplateSecurityToJSON(deleteTemplateSecurity: DeleteTemplateSecurity): string;
/** @internal */
export type DeleteTemplateRequest$Outbound = {
    id?: string | undefined;
    name?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteTemplateRequest$outboundSchema: z.ZodMiniType<DeleteTemplateRequest$Outbound, DeleteTemplateRequest>;
export declare function deleteTemplateRequestToJSON(deleteTemplateRequest: DeleteTemplateRequest): string;
//# sourceMappingURL=deletetemplate.d.ts.map