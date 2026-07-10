import * as z from "zod/v4-mini";
import { SearchToolCallsPayload, SearchToolCallsPayload$Outbound } from "../components/searchtoolcallspayload.js";
export type SearchToolCallsSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type SearchToolCallsSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type SearchToolCallsSecurity = {
    option1?: SearchToolCallsSecurityOption1 | undefined;
    option2?: SearchToolCallsSecurityOption2 | undefined;
};
export type SearchToolCallsRequest = {
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
    searchToolCallsPayload: SearchToolCallsPayload;
};
/** @internal */
export type SearchToolCallsSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const SearchToolCallsSecurityOption1$outboundSchema: z.ZodMiniType<SearchToolCallsSecurityOption1$Outbound, SearchToolCallsSecurityOption1>;
export declare function searchToolCallsSecurityOption1ToJSON(searchToolCallsSecurityOption1: SearchToolCallsSecurityOption1): string;
/** @internal */
export type SearchToolCallsSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const SearchToolCallsSecurityOption2$outboundSchema: z.ZodMiniType<SearchToolCallsSecurityOption2$Outbound, SearchToolCallsSecurityOption2>;
export declare function searchToolCallsSecurityOption2ToJSON(searchToolCallsSecurityOption2: SearchToolCallsSecurityOption2): string;
/** @internal */
export type SearchToolCallsSecurity$Outbound = {
    Option1?: SearchToolCallsSecurityOption1$Outbound | undefined;
    Option2?: SearchToolCallsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const SearchToolCallsSecurity$outboundSchema: z.ZodMiniType<SearchToolCallsSecurity$Outbound, SearchToolCallsSecurity>;
export declare function searchToolCallsSecurityToJSON(searchToolCallsSecurity: SearchToolCallsSecurity): string;
/** @internal */
export type SearchToolCallsRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    SearchToolCallsPayload: SearchToolCallsPayload$Outbound;
};
/** @internal */
export declare const SearchToolCallsRequest$outboundSchema: z.ZodMiniType<SearchToolCallsRequest$Outbound, SearchToolCallsRequest>;
export declare function searchToolCallsRequestToJSON(searchToolCallsRequest: SearchToolCallsRequest): string;
//# sourceMappingURL=searchtoolcalls.d.ts.map