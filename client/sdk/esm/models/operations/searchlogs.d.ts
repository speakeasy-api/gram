import * as z from "zod/v4-mini";
import { SearchLogsPayload, SearchLogsPayload$Outbound } from "../components/searchlogspayload.js";
export type SearchLogsSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type SearchLogsSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type SearchLogsSecurity = {
    option1?: SearchLogsSecurityOption1 | undefined;
    option2?: SearchLogsSecurityOption2 | undefined;
};
export type SearchLogsRequest = {
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
    searchLogsPayload: SearchLogsPayload;
};
/** @internal */
export type SearchLogsSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const SearchLogsSecurityOption1$outboundSchema: z.ZodMiniType<SearchLogsSecurityOption1$Outbound, SearchLogsSecurityOption1>;
export declare function searchLogsSecurityOption1ToJSON(searchLogsSecurityOption1: SearchLogsSecurityOption1): string;
/** @internal */
export type SearchLogsSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const SearchLogsSecurityOption2$outboundSchema: z.ZodMiniType<SearchLogsSecurityOption2$Outbound, SearchLogsSecurityOption2>;
export declare function searchLogsSecurityOption2ToJSON(searchLogsSecurityOption2: SearchLogsSecurityOption2): string;
/** @internal */
export type SearchLogsSecurity$Outbound = {
    Option1?: SearchLogsSecurityOption1$Outbound | undefined;
    Option2?: SearchLogsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const SearchLogsSecurity$outboundSchema: z.ZodMiniType<SearchLogsSecurity$Outbound, SearchLogsSecurity>;
export declare function searchLogsSecurityToJSON(searchLogsSecurity: SearchLogsSecurity): string;
/** @internal */
export type SearchLogsRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    SearchLogsPayload: SearchLogsPayload$Outbound;
};
/** @internal */
export declare const SearchLogsRequest$outboundSchema: z.ZodMiniType<SearchLogsRequest$Outbound, SearchLogsRequest>;
export declare function searchLogsRequestToJSON(searchLogsRequest: SearchLogsRequest): string;
//# sourceMappingURL=searchlogs.d.ts.map