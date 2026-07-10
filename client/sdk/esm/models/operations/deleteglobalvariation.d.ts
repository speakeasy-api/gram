import * as z from "zod/v4-mini";
export type DeleteGlobalVariationSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type DeleteGlobalVariationSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type DeleteGlobalVariationSecurity = {
    option1?: DeleteGlobalVariationSecurityOption1 | undefined;
    option2?: DeleteGlobalVariationSecurityOption2 | undefined;
};
export type DeleteGlobalVariationRequest = {
    /**
     * The ID of the variation to delete
     */
    variationId: string;
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
export type DeleteGlobalVariationSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const DeleteGlobalVariationSecurityOption1$outboundSchema: z.ZodMiniType<DeleteGlobalVariationSecurityOption1$Outbound, DeleteGlobalVariationSecurityOption1>;
export declare function deleteGlobalVariationSecurityOption1ToJSON(deleteGlobalVariationSecurityOption1: DeleteGlobalVariationSecurityOption1): string;
/** @internal */
export type DeleteGlobalVariationSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DeleteGlobalVariationSecurityOption2$outboundSchema: z.ZodMiniType<DeleteGlobalVariationSecurityOption2$Outbound, DeleteGlobalVariationSecurityOption2>;
export declare function deleteGlobalVariationSecurityOption2ToJSON(deleteGlobalVariationSecurityOption2: DeleteGlobalVariationSecurityOption2): string;
/** @internal */
export type DeleteGlobalVariationSecurity$Outbound = {
    Option1?: DeleteGlobalVariationSecurityOption1$Outbound | undefined;
    Option2?: DeleteGlobalVariationSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DeleteGlobalVariationSecurity$outboundSchema: z.ZodMiniType<DeleteGlobalVariationSecurity$Outbound, DeleteGlobalVariationSecurity>;
export declare function deleteGlobalVariationSecurityToJSON(deleteGlobalVariationSecurity: DeleteGlobalVariationSecurity): string;
/** @internal */
export type DeleteGlobalVariationRequest$Outbound = {
    variation_id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteGlobalVariationRequest$outboundSchema: z.ZodMiniType<DeleteGlobalVariationRequest$Outbound, DeleteGlobalVariationRequest>;
export declare function deleteGlobalVariationRequestToJSON(deleteGlobalVariationRequest: DeleteGlobalVariationRequest): string;
//# sourceMappingURL=deleteglobalvariation.d.ts.map