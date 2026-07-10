import * as z from "zod/v4-mini";
export type ListGlobalVariationsSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListGlobalVariationsSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListGlobalVariationsSecurity = {
    option1?: ListGlobalVariationsSecurityOption1 | undefined;
    option2?: ListGlobalVariationsSecurityOption2 | undefined;
};
export type ListGlobalVariationsRequest = {
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
export type ListGlobalVariationsSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListGlobalVariationsSecurityOption1$outboundSchema: z.ZodMiniType<ListGlobalVariationsSecurityOption1$Outbound, ListGlobalVariationsSecurityOption1>;
export declare function listGlobalVariationsSecurityOption1ToJSON(listGlobalVariationsSecurityOption1: ListGlobalVariationsSecurityOption1): string;
/** @internal */
export type ListGlobalVariationsSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListGlobalVariationsSecurityOption2$outboundSchema: z.ZodMiniType<ListGlobalVariationsSecurityOption2$Outbound, ListGlobalVariationsSecurityOption2>;
export declare function listGlobalVariationsSecurityOption2ToJSON(listGlobalVariationsSecurityOption2: ListGlobalVariationsSecurityOption2): string;
/** @internal */
export type ListGlobalVariationsSecurity$Outbound = {
    Option1?: ListGlobalVariationsSecurityOption1$Outbound | undefined;
    Option2?: ListGlobalVariationsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListGlobalVariationsSecurity$outboundSchema: z.ZodMiniType<ListGlobalVariationsSecurity$Outbound, ListGlobalVariationsSecurity>;
export declare function listGlobalVariationsSecurityToJSON(listGlobalVariationsSecurity: ListGlobalVariationsSecurity): string;
/** @internal */
export type ListGlobalVariationsRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListGlobalVariationsRequest$outboundSchema: z.ZodMiniType<ListGlobalVariationsRequest$Outbound, ListGlobalVariationsRequest>;
export declare function listGlobalVariationsRequestToJSON(listGlobalVariationsRequest: ListGlobalVariationsRequest): string;
//# sourceMappingURL=listglobalvariations.d.ts.map