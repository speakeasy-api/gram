import * as z from "zod/v4-mini";
export type ListRiskEvalReviewsSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListRiskEvalReviewsSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListRiskEvalReviewsSecurity = {
    option1?: ListRiskEvalReviewsSecurityOption1 | undefined;
    option2?: ListRiskEvalReviewsSecurityOption2 | undefined;
};
export type ListRiskEvalReviewsRequest = {
    /**
     * The policy whose review set to list.
     */
    policyId: string;
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
export type ListRiskEvalReviewsSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListRiskEvalReviewsSecurityOption1$outboundSchema: z.ZodMiniType<ListRiskEvalReviewsSecurityOption1$Outbound, ListRiskEvalReviewsSecurityOption1>;
export declare function listRiskEvalReviewsSecurityOption1ToJSON(listRiskEvalReviewsSecurityOption1: ListRiskEvalReviewsSecurityOption1): string;
/** @internal */
export type ListRiskEvalReviewsSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListRiskEvalReviewsSecurityOption2$outboundSchema: z.ZodMiniType<ListRiskEvalReviewsSecurityOption2$Outbound, ListRiskEvalReviewsSecurityOption2>;
export declare function listRiskEvalReviewsSecurityOption2ToJSON(listRiskEvalReviewsSecurityOption2: ListRiskEvalReviewsSecurityOption2): string;
/** @internal */
export type ListRiskEvalReviewsSecurity$Outbound = {
    Option1?: ListRiskEvalReviewsSecurityOption1$Outbound | undefined;
    Option2?: ListRiskEvalReviewsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListRiskEvalReviewsSecurity$outboundSchema: z.ZodMiniType<ListRiskEvalReviewsSecurity$Outbound, ListRiskEvalReviewsSecurity>;
export declare function listRiskEvalReviewsSecurityToJSON(listRiskEvalReviewsSecurity: ListRiskEvalReviewsSecurity): string;
/** @internal */
export type ListRiskEvalReviewsRequest$Outbound = {
    policy_id: string;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListRiskEvalReviewsRequest$outboundSchema: z.ZodMiniType<ListRiskEvalReviewsRequest$Outbound, ListRiskEvalReviewsRequest>;
export declare function listRiskEvalReviewsRequestToJSON(listRiskEvalReviewsRequest: ListRiskEvalReviewsRequest): string;
//# sourceMappingURL=listriskevalreviews.d.ts.map