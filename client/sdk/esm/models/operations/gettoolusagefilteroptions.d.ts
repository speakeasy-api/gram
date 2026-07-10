import * as z from "zod/v4-mini";
import { GetToolUsageFilterOptionsPayload, GetToolUsageFilterOptionsPayload$Outbound } from "../components/gettoolusagefilteroptionspayload.js";
export type GetToolUsageFilterOptionsSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetToolUsageFilterOptionsSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetToolUsageFilterOptionsSecurity = {
    option1?: GetToolUsageFilterOptionsSecurityOption1 | undefined;
    option2?: GetToolUsageFilterOptionsSecurityOption2 | undefined;
};
export type GetToolUsageFilterOptionsRequest = {
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
    getToolUsageFilterOptionsPayload: GetToolUsageFilterOptionsPayload;
};
/** @internal */
export type GetToolUsageFilterOptionsSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetToolUsageFilterOptionsSecurityOption1$outboundSchema: z.ZodMiniType<GetToolUsageFilterOptionsSecurityOption1$Outbound, GetToolUsageFilterOptionsSecurityOption1>;
export declare function getToolUsageFilterOptionsSecurityOption1ToJSON(getToolUsageFilterOptionsSecurityOption1: GetToolUsageFilterOptionsSecurityOption1): string;
/** @internal */
export type GetToolUsageFilterOptionsSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetToolUsageFilterOptionsSecurityOption2$outboundSchema: z.ZodMiniType<GetToolUsageFilterOptionsSecurityOption2$Outbound, GetToolUsageFilterOptionsSecurityOption2>;
export declare function getToolUsageFilterOptionsSecurityOption2ToJSON(getToolUsageFilterOptionsSecurityOption2: GetToolUsageFilterOptionsSecurityOption2): string;
/** @internal */
export type GetToolUsageFilterOptionsSecurity$Outbound = {
    Option1?: GetToolUsageFilterOptionsSecurityOption1$Outbound | undefined;
    Option2?: GetToolUsageFilterOptionsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetToolUsageFilterOptionsSecurity$outboundSchema: z.ZodMiniType<GetToolUsageFilterOptionsSecurity$Outbound, GetToolUsageFilterOptionsSecurity>;
export declare function getToolUsageFilterOptionsSecurityToJSON(getToolUsageFilterOptionsSecurity: GetToolUsageFilterOptionsSecurity): string;
/** @internal */
export type GetToolUsageFilterOptionsRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    GetToolUsageFilterOptionsPayload: GetToolUsageFilterOptionsPayload$Outbound;
};
/** @internal */
export declare const GetToolUsageFilterOptionsRequest$outboundSchema: z.ZodMiniType<GetToolUsageFilterOptionsRequest$Outbound, GetToolUsageFilterOptionsRequest>;
export declare function getToolUsageFilterOptionsRequestToJSON(getToolUsageFilterOptionsRequest: GetToolUsageFilterOptionsRequest): string;
//# sourceMappingURL=gettoolusagefilteroptions.d.ts.map