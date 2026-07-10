import * as z from "zod/v4-mini";
import { GetProjectMetricsSummaryPayload, GetProjectMetricsSummaryPayload$Outbound } from "../components/getprojectmetricssummarypayload.js";
export type GetProjectOverviewSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetProjectOverviewSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetProjectOverviewSecurity = {
    option1?: GetProjectOverviewSecurityOption1 | undefined;
    option2?: GetProjectOverviewSecurityOption2 | undefined;
};
export type GetProjectOverviewRequest = {
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
export type GetProjectOverviewSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetProjectOverviewSecurityOption1$outboundSchema: z.ZodMiniType<GetProjectOverviewSecurityOption1$Outbound, GetProjectOverviewSecurityOption1>;
export declare function getProjectOverviewSecurityOption1ToJSON(getProjectOverviewSecurityOption1: GetProjectOverviewSecurityOption1): string;
/** @internal */
export type GetProjectOverviewSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetProjectOverviewSecurityOption2$outboundSchema: z.ZodMiniType<GetProjectOverviewSecurityOption2$Outbound, GetProjectOverviewSecurityOption2>;
export declare function getProjectOverviewSecurityOption2ToJSON(getProjectOverviewSecurityOption2: GetProjectOverviewSecurityOption2): string;
/** @internal */
export type GetProjectOverviewSecurity$Outbound = {
    Option1?: GetProjectOverviewSecurityOption1$Outbound | undefined;
    Option2?: GetProjectOverviewSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetProjectOverviewSecurity$outboundSchema: z.ZodMiniType<GetProjectOverviewSecurity$Outbound, GetProjectOverviewSecurity>;
export declare function getProjectOverviewSecurityToJSON(getProjectOverviewSecurity: GetProjectOverviewSecurity): string;
/** @internal */
export type GetProjectOverviewRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    GetProjectMetricsSummaryPayload: GetProjectMetricsSummaryPayload$Outbound;
};
/** @internal */
export declare const GetProjectOverviewRequest$outboundSchema: z.ZodMiniType<GetProjectOverviewRequest$Outbound, GetProjectOverviewRequest>;
export declare function getProjectOverviewRequestToJSON(getProjectOverviewRequest: GetProjectOverviewRequest): string;
//# sourceMappingURL=getprojectoverview.d.ts.map