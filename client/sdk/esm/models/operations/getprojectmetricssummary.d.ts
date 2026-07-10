import * as z from "zod/v4-mini";
import { GetProjectMetricsSummaryPayload, GetProjectMetricsSummaryPayload$Outbound } from "../components/getprojectmetricssummarypayload.js";
export type GetProjectMetricsSummarySecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetProjectMetricsSummarySecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetProjectMetricsSummarySecurity = {
    option1?: GetProjectMetricsSummarySecurityOption1 | undefined;
    option2?: GetProjectMetricsSummarySecurityOption2 | undefined;
};
export type GetProjectMetricsSummaryRequest = {
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
    getProjectMetricsSummaryPayload: GetProjectMetricsSummaryPayload;
};
/** @internal */
export type GetProjectMetricsSummarySecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetProjectMetricsSummarySecurityOption1$outboundSchema: z.ZodMiniType<GetProjectMetricsSummarySecurityOption1$Outbound, GetProjectMetricsSummarySecurityOption1>;
export declare function getProjectMetricsSummarySecurityOption1ToJSON(getProjectMetricsSummarySecurityOption1: GetProjectMetricsSummarySecurityOption1): string;
/** @internal */
export type GetProjectMetricsSummarySecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetProjectMetricsSummarySecurityOption2$outboundSchema: z.ZodMiniType<GetProjectMetricsSummarySecurityOption2$Outbound, GetProjectMetricsSummarySecurityOption2>;
export declare function getProjectMetricsSummarySecurityOption2ToJSON(getProjectMetricsSummarySecurityOption2: GetProjectMetricsSummarySecurityOption2): string;
/** @internal */
export type GetProjectMetricsSummarySecurity$Outbound = {
    Option1?: GetProjectMetricsSummarySecurityOption1$Outbound | undefined;
    Option2?: GetProjectMetricsSummarySecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetProjectMetricsSummarySecurity$outboundSchema: z.ZodMiniType<GetProjectMetricsSummarySecurity$Outbound, GetProjectMetricsSummarySecurity>;
export declare function getProjectMetricsSummarySecurityToJSON(getProjectMetricsSummarySecurity: GetProjectMetricsSummarySecurity): string;
/** @internal */
export type GetProjectMetricsSummaryRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    GetProjectMetricsSummaryPayload: GetProjectMetricsSummaryPayload$Outbound;
};
/** @internal */
export declare const GetProjectMetricsSummaryRequest$outboundSchema: z.ZodMiniType<GetProjectMetricsSummaryRequest$Outbound, GetProjectMetricsSummaryRequest>;
export declare function getProjectMetricsSummaryRequestToJSON(getProjectMetricsSummaryRequest: GetProjectMetricsSummaryRequest): string;
//# sourceMappingURL=getprojectmetricssummary.d.ts.map