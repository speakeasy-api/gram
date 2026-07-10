import * as z from "zod/v4-mini";
import { GetToolUsageSummaryPayload, GetToolUsageSummaryPayload$Outbound } from "../components/gettoolusagesummarypayload.js";
export type GetToolUsageSummarySecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetToolUsageSummarySecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetToolUsageSummarySecurity = {
    option1?: GetToolUsageSummarySecurityOption1 | undefined;
    option2?: GetToolUsageSummarySecurityOption2 | undefined;
};
export type GetToolUsageSummaryRequest = {
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
    getToolUsageSummaryPayload: GetToolUsageSummaryPayload;
};
/** @internal */
export type GetToolUsageSummarySecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetToolUsageSummarySecurityOption1$outboundSchema: z.ZodMiniType<GetToolUsageSummarySecurityOption1$Outbound, GetToolUsageSummarySecurityOption1>;
export declare function getToolUsageSummarySecurityOption1ToJSON(getToolUsageSummarySecurityOption1: GetToolUsageSummarySecurityOption1): string;
/** @internal */
export type GetToolUsageSummarySecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetToolUsageSummarySecurityOption2$outboundSchema: z.ZodMiniType<GetToolUsageSummarySecurityOption2$Outbound, GetToolUsageSummarySecurityOption2>;
export declare function getToolUsageSummarySecurityOption2ToJSON(getToolUsageSummarySecurityOption2: GetToolUsageSummarySecurityOption2): string;
/** @internal */
export type GetToolUsageSummarySecurity$Outbound = {
    Option1?: GetToolUsageSummarySecurityOption1$Outbound | undefined;
    Option2?: GetToolUsageSummarySecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetToolUsageSummarySecurity$outboundSchema: z.ZodMiniType<GetToolUsageSummarySecurity$Outbound, GetToolUsageSummarySecurity>;
export declare function getToolUsageSummarySecurityToJSON(getToolUsageSummarySecurity: GetToolUsageSummarySecurity): string;
/** @internal */
export type GetToolUsageSummaryRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    GetToolUsageSummaryPayload: GetToolUsageSummaryPayload$Outbound;
};
/** @internal */
export declare const GetToolUsageSummaryRequest$outboundSchema: z.ZodMiniType<GetToolUsageSummaryRequest$Outbound, GetToolUsageSummaryRequest>;
export declare function getToolUsageSummaryRequestToJSON(getToolUsageSummaryRequest: GetToolUsageSummaryRequest): string;
//# sourceMappingURL=gettoolusagesummary.d.ts.map