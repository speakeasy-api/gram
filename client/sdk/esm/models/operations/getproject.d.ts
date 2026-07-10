import * as z from "zod/v4-mini";
export type GetProjectSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type GetProjectRequest = {
    /**
     * The slug of the project to get
     */
    slug: string;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type GetProjectSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetProjectSecurity$outboundSchema: z.ZodMiniType<GetProjectSecurity$Outbound, GetProjectSecurity>;
export declare function getProjectSecurityToJSON(getProjectSecurity: GetProjectSecurity): string;
/** @internal */
export type GetProjectRequest$Outbound = {
    slug: string;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetProjectRequest$outboundSchema: z.ZodMiniType<GetProjectRequest$Outbound, GetProjectRequest>;
export declare function getProjectRequestToJSON(getProjectRequest: GetProjectRequest): string;
//# sourceMappingURL=getproject.d.ts.map