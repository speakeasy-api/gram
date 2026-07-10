import * as z from "zod/v4-mini";
export type GetSlackAppSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type GetSlackAppRequest = {
    /**
     * The Slack app ID
     */
    id: string;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
};
/** @internal */
export type GetSlackAppSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetSlackAppSecurity$outboundSchema: z.ZodMiniType<GetSlackAppSecurity$Outbound, GetSlackAppSecurity>;
export declare function getSlackAppSecurityToJSON(getSlackAppSecurity: GetSlackAppSecurity): string;
/** @internal */
export type GetSlackAppRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetSlackAppRequest$outboundSchema: z.ZodMiniType<GetSlackAppRequest$Outbound, GetSlackAppRequest>;
export declare function getSlackAppRequestToJSON(getSlackAppRequest: GetSlackAppRequest): string;
//# sourceMappingURL=getslackapp.d.ts.map