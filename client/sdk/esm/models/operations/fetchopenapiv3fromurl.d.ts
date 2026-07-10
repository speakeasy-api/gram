import * as z from "zod/v4-mini";
import { FetchOpenAPIv3FromURLForm2, FetchOpenAPIv3FromURLForm2$Outbound } from "../components/fetchopenapiv3fromurlform2.js";
export type FetchOpenAPIv3FromURLSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type FetchOpenAPIv3FromURLSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type FetchOpenAPIv3FromURLSecurity = {
    option1?: FetchOpenAPIv3FromURLSecurityOption1 | undefined;
    option2?: FetchOpenAPIv3FromURLSecurityOption2 | undefined;
};
export type FetchOpenAPIv3FromURLRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    fetchOpenAPIv3FromURLForm2: FetchOpenAPIv3FromURLForm2;
};
/** @internal */
export type FetchOpenAPIv3FromURLSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const FetchOpenAPIv3FromURLSecurityOption1$outboundSchema: z.ZodMiniType<FetchOpenAPIv3FromURLSecurityOption1$Outbound, FetchOpenAPIv3FromURLSecurityOption1>;
export declare function fetchOpenAPIv3FromURLSecurityOption1ToJSON(fetchOpenAPIv3FromURLSecurityOption1: FetchOpenAPIv3FromURLSecurityOption1): string;
/** @internal */
export type FetchOpenAPIv3FromURLSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const FetchOpenAPIv3FromURLSecurityOption2$outboundSchema: z.ZodMiniType<FetchOpenAPIv3FromURLSecurityOption2$Outbound, FetchOpenAPIv3FromURLSecurityOption2>;
export declare function fetchOpenAPIv3FromURLSecurityOption2ToJSON(fetchOpenAPIv3FromURLSecurityOption2: FetchOpenAPIv3FromURLSecurityOption2): string;
/** @internal */
export type FetchOpenAPIv3FromURLSecurity$Outbound = {
    Option1?: FetchOpenAPIv3FromURLSecurityOption1$Outbound | undefined;
    Option2?: FetchOpenAPIv3FromURLSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const FetchOpenAPIv3FromURLSecurity$outboundSchema: z.ZodMiniType<FetchOpenAPIv3FromURLSecurity$Outbound, FetchOpenAPIv3FromURLSecurity>;
export declare function fetchOpenAPIv3FromURLSecurityToJSON(fetchOpenAPIv3FromURLSecurity: FetchOpenAPIv3FromURLSecurity): string;
/** @internal */
export type FetchOpenAPIv3FromURLRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    "Gram-Session"?: string | undefined;
    FetchOpenAPIv3FromURLForm2: FetchOpenAPIv3FromURLForm2$Outbound;
};
/** @internal */
export declare const FetchOpenAPIv3FromURLRequest$outboundSchema: z.ZodMiniType<FetchOpenAPIv3FromURLRequest$Outbound, FetchOpenAPIv3FromURLRequest>;
export declare function fetchOpenAPIv3FromURLRequestToJSON(fetchOpenAPIv3FromURLRequest: FetchOpenAPIv3FromURLRequest): string;
//# sourceMappingURL=fetchopenapiv3fromurl.d.ts.map