import * as z from "zod/v4-mini";
import { GetUserMetricsSummaryPayload, GetUserMetricsSummaryPayload$Outbound } from "../components/getusermetricssummarypayload.js";
export type GetUserMetricsSummarySecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetUserMetricsSummarySecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetUserMetricsSummarySecurity = {
    option1?: GetUserMetricsSummarySecurityOption1 | undefined;
    option2?: GetUserMetricsSummarySecurityOption2 | undefined;
};
export type GetUserMetricsSummaryRequest = {
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
    getUserMetricsSummaryPayload: GetUserMetricsSummaryPayload;
};
/** @internal */
export type GetUserMetricsSummarySecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetUserMetricsSummarySecurityOption1$outboundSchema: z.ZodMiniType<GetUserMetricsSummarySecurityOption1$Outbound, GetUserMetricsSummarySecurityOption1>;
export declare function getUserMetricsSummarySecurityOption1ToJSON(getUserMetricsSummarySecurityOption1: GetUserMetricsSummarySecurityOption1): string;
/** @internal */
export type GetUserMetricsSummarySecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetUserMetricsSummarySecurityOption2$outboundSchema: z.ZodMiniType<GetUserMetricsSummarySecurityOption2$Outbound, GetUserMetricsSummarySecurityOption2>;
export declare function getUserMetricsSummarySecurityOption2ToJSON(getUserMetricsSummarySecurityOption2: GetUserMetricsSummarySecurityOption2): string;
/** @internal */
export type GetUserMetricsSummarySecurity$Outbound = {
    Option1?: GetUserMetricsSummarySecurityOption1$Outbound | undefined;
    Option2?: GetUserMetricsSummarySecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetUserMetricsSummarySecurity$outboundSchema: z.ZodMiniType<GetUserMetricsSummarySecurity$Outbound, GetUserMetricsSummarySecurity>;
export declare function getUserMetricsSummarySecurityToJSON(getUserMetricsSummarySecurity: GetUserMetricsSummarySecurity): string;
/** @internal */
export type GetUserMetricsSummaryRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    GetUserMetricsSummaryPayload: GetUserMetricsSummaryPayload$Outbound;
};
/** @internal */
export declare const GetUserMetricsSummaryRequest$outboundSchema: z.ZodMiniType<GetUserMetricsSummaryRequest$Outbound, GetUserMetricsSummaryRequest>;
export declare function getUserMetricsSummaryRequestToJSON(getUserMetricsSummaryRequest: GetUserMetricsSummaryRequest): string;
//# sourceMappingURL=getusermetricssummary.d.ts.map