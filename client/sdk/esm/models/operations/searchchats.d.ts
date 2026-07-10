import * as z from "zod/v4-mini";
import { SearchChatsPayload, SearchChatsPayload$Outbound } from "../components/searchchatspayload.js";
export type SearchChatsSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type SearchChatsSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type SearchChatsSecurity = {
    option1?: SearchChatsSecurityOption1 | undefined;
    option2?: SearchChatsSecurityOption2 | undefined;
};
export type SearchChatsRequest = {
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
    searchChatsPayload: SearchChatsPayload;
};
/** @internal */
export type SearchChatsSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const SearchChatsSecurityOption1$outboundSchema: z.ZodMiniType<SearchChatsSecurityOption1$Outbound, SearchChatsSecurityOption1>;
export declare function searchChatsSecurityOption1ToJSON(searchChatsSecurityOption1: SearchChatsSecurityOption1): string;
/** @internal */
export type SearchChatsSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const SearchChatsSecurityOption2$outboundSchema: z.ZodMiniType<SearchChatsSecurityOption2$Outbound, SearchChatsSecurityOption2>;
export declare function searchChatsSecurityOption2ToJSON(searchChatsSecurityOption2: SearchChatsSecurityOption2): string;
/** @internal */
export type SearchChatsSecurity$Outbound = {
    Option1?: SearchChatsSecurityOption1$Outbound | undefined;
    Option2?: SearchChatsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const SearchChatsSecurity$outboundSchema: z.ZodMiniType<SearchChatsSecurity$Outbound, SearchChatsSecurity>;
export declare function searchChatsSecurityToJSON(searchChatsSecurity: SearchChatsSecurity): string;
/** @internal */
export type SearchChatsRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    SearchChatsPayload: SearchChatsPayload$Outbound;
};
/** @internal */
export declare const SearchChatsRequest$outboundSchema: z.ZodMiniType<SearchChatsRequest$Outbound, SearchChatsRequest>;
export declare function searchChatsRequestToJSON(searchChatsRequest: SearchChatsRequest): string;
//# sourceMappingURL=searchchats.d.ts.map