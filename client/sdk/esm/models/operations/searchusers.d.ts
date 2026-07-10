import * as z from "zod/v4-mini";
import { SearchUsersPayload, SearchUsersPayload$Outbound } from "../components/searchuserspayload.js";
export type SearchUsersSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type SearchUsersSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type SearchUsersSecurity = {
    option1?: SearchUsersSecurityOption1 | undefined;
    option2?: SearchUsersSecurityOption2 | undefined;
};
export type SearchUsersRequest = {
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
    searchUsersPayload: SearchUsersPayload;
};
/** @internal */
export type SearchUsersSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const SearchUsersSecurityOption1$outboundSchema: z.ZodMiniType<SearchUsersSecurityOption1$Outbound, SearchUsersSecurityOption1>;
export declare function searchUsersSecurityOption1ToJSON(searchUsersSecurityOption1: SearchUsersSecurityOption1): string;
/** @internal */
export type SearchUsersSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const SearchUsersSecurityOption2$outboundSchema: z.ZodMiniType<SearchUsersSecurityOption2$Outbound, SearchUsersSecurityOption2>;
export declare function searchUsersSecurityOption2ToJSON(searchUsersSecurityOption2: SearchUsersSecurityOption2): string;
/** @internal */
export type SearchUsersSecurity$Outbound = {
    Option1?: SearchUsersSecurityOption1$Outbound | undefined;
    Option2?: SearchUsersSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const SearchUsersSecurity$outboundSchema: z.ZodMiniType<SearchUsersSecurity$Outbound, SearchUsersSecurity>;
export declare function searchUsersSecurityToJSON(searchUsersSecurity: SearchUsersSecurity): string;
/** @internal */
export type SearchUsersRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    SearchUsersPayload: SearchUsersPayload$Outbound;
};
/** @internal */
export declare const SearchUsersRequest$outboundSchema: z.ZodMiniType<SearchUsersRequest$Outbound, SearchUsersRequest>;
export declare function searchUsersRequestToJSON(searchUsersRequest: SearchUsersRequest): string;
//# sourceMappingURL=searchusers.d.ts.map