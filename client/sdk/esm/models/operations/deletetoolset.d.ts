import * as z from "zod/v4-mini";
export type DeleteToolsetSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type DeleteToolsetSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type DeleteToolsetSecurity = {
    option1?: DeleteToolsetSecurityOption1 | undefined;
    option2?: DeleteToolsetSecurityOption2 | undefined;
};
export type DeleteToolsetRequest = {
    /**
     * The slug of the toolset
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
};
/** @internal */
export type DeleteToolsetSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const DeleteToolsetSecurityOption1$outboundSchema: z.ZodMiniType<DeleteToolsetSecurityOption1$Outbound, DeleteToolsetSecurityOption1>;
export declare function deleteToolsetSecurityOption1ToJSON(deleteToolsetSecurityOption1: DeleteToolsetSecurityOption1): string;
/** @internal */
export type DeleteToolsetSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DeleteToolsetSecurityOption2$outboundSchema: z.ZodMiniType<DeleteToolsetSecurityOption2$Outbound, DeleteToolsetSecurityOption2>;
export declare function deleteToolsetSecurityOption2ToJSON(deleteToolsetSecurityOption2: DeleteToolsetSecurityOption2): string;
/** @internal */
export type DeleteToolsetSecurity$Outbound = {
    Option1?: DeleteToolsetSecurityOption1$Outbound | undefined;
    Option2?: DeleteToolsetSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DeleteToolsetSecurity$outboundSchema: z.ZodMiniType<DeleteToolsetSecurity$Outbound, DeleteToolsetSecurity>;
export declare function deleteToolsetSecurityToJSON(deleteToolsetSecurity: DeleteToolsetSecurity): string;
/** @internal */
export type DeleteToolsetRequest$Outbound = {
    slug: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteToolsetRequest$outboundSchema: z.ZodMiniType<DeleteToolsetRequest$Outbound, DeleteToolsetRequest>;
export declare function deleteToolsetRequestToJSON(deleteToolsetRequest: DeleteToolsetRequest): string;
//# sourceMappingURL=deletetoolset.d.ts.map